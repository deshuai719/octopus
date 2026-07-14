import { beforeEach, describe, expect, it, vi } from "vitest";
import { extractNewAPICredentials } from "../src/adapters/newapi";

function storageWith(values: Record<string, string>): Storage {
  return {
    getItem: (key: string) => values[key] ?? null,
  } as Storage;
}

function jsonResponse(status: number, payload?: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: vi.fn(async () => payload),
  } as unknown as Response;
}

describe("NewAPI credential extraction", () => {
  beforeEach(() => {
    vi.stubGlobal("location", { origin: "https://newapi.example.com" });
    vi.stubGlobal("document", {
      querySelector: vi.fn(() => null),
      querySelectorAll: vi.fn(() => []),
    });
    vi.stubGlobal("localStorage", storageWith({
      user: JSON.stringify({ id: 42, username: "alice", token: "browser-session-token" }),
      system_access_token: "system-access-token-123456",
    }));
    vi.stubGlobal("sessionStorage", storageWith({}));
  });

  it("uses the NewAPI user id header when probing the logged-in profile", async () => {
    const fetchProfile = vi.fn(async (_input: URL | RequestInfo, init?: RequestInit) => {
      const headers = init?.headers as Record<string, string> | undefined;
      if (headers?.["New-API-User"] !== "42") return jsonResponse(401);
      return jsonResponse(200, { data: { id: 42, username: "alice" } });
    });
    vi.stubGlobal("fetch", fetchProfile);

    await expect(extractNewAPICredentials()).resolves.toEqual({
      kind: "candidate",
      candidate: {
        access_token: "system-access-token-123456",
        platform_user_id: 42,
        identity_label: "alice",
      },
    });
    expect(fetchProfile).toHaveBeenCalledTimes(1);
  });

  it("tries the next compatible profile endpoint after one endpoint returns 401", async () => {
    const fetchProfile = vi.fn(async (input: URL | RequestInfo) => {
      const path = new URL(String(input)).pathname;
      if (path === "/api/user/self") return jsonResponse(401);
      return jsonResponse(200, { data: { user: { id: 42, username: "alice" } } });
    });
    vi.stubGlobal("fetch", fetchProfile);

    await expect(extractNewAPICredentials()).resolves.toMatchObject({
      kind: "candidate",
      candidate: {
        access_token: "system-access-token-123456",
        platform_user_id: 42,
      },
    });
    expect(fetchProfile).toHaveBeenCalledTimes(2);
  });

  it("reads a manually generated token only from the labeled system-token card", async () => {
    class FakeInput {
      value = "system-token-rendered-by-newapi";
    }
    class FakeTextArea {
      value = "";
    }
    const tokenInput = new FakeInput();
    const card = {
      querySelector: vi.fn(() => tokenInput),
      parentElement: null,
    };
    const label = {
      textContent: "系统访问令牌",
      parentElement: card,
    };
    vi.stubGlobal("HTMLInputElement", FakeInput);
    vi.stubGlobal("HTMLTextAreaElement", FakeTextArea);
    vi.stubGlobal("document", {
      querySelector: vi.fn(() => null),
      querySelectorAll: vi.fn(() => [label]),
    });
    vi.stubGlobal("localStorage", storageWith({
      user: JSON.stringify({ id: 42, username: "alice" }),
    }));
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse(200, { data: { id: 42, username: "alice" } })));

    await expect(extractNewAPICredentials()).resolves.toMatchObject({
      kind: "candidate",
      candidate: {
        access_token: "system-token-rendered-by-newapi",
        platform_user_id: 42,
      },
    });
    expect(card.querySelector).toHaveBeenCalledWith("input[readonly], textarea[readonly]");
  });

  it("stays read-only by default and reports a machine-readable missing-token reason", async () => {
    vi.stubGlobal("localStorage", storageWith({
      user: JSON.stringify({ id: 42, username: "alice" }),
    }));
    const fetchProfile = vi.fn(async () => jsonResponse(200, { data: { id: 42, username: "alice" } }));
    vi.stubGlobal("fetch", fetchProfile);

    await expect(extractNewAPICredentials()).resolves.toMatchObject({
      kind: "manual_required",
      reason: "system_token_missing",
    });
    expect(fetchProfile).toHaveBeenCalledTimes(1);
  });

  it("generates one system token only when the worker explicitly allows it", async () => {
    vi.stubGlobal("localStorage", storageWith({
      user: JSON.stringify({ id: 42, username: "alice", token: "browser-session-token" }),
    }));
    const fetchNewAPI = vi.fn(async (input: URL | RequestInfo) => {
      const path = new URL(String(input)).pathname;
      if (path === "/api/user/token") {
        return jsonResponse(200, { success: true, data: "new-system-token-123456789" });
      }
      return jsonResponse(200, { data: { id: 42, username: "alice" } });
    });
    vi.stubGlobal("fetch", fetchNewAPI);

    await expect(extractNewAPICredentials({ allow_token_generation: true })).resolves.toEqual({
      kind: "candidate",
      generated_system_token: true,
      candidate: {
        access_token: "new-system-token-123456789",
        platform_user_id: 42,
        identity_label: "alice",
      },
    });
    expect(fetchNewAPI).toHaveBeenCalledTimes(2);
    expect(fetchNewAPI.mock.calls.map(([input]) => new URL(String(input)).pathname)).toEqual([
      "/api/user/self",
      "/api/user/token",
    ]);
  });

  it("does not generate when an existing complete system token is available", async () => {
    const fetchProfile = vi.fn(async () => jsonResponse(200, { data: { id: 42, username: "alice" } }));
    vi.stubGlobal("fetch", fetchProfile);

    await expect(extractNewAPICredentials({ allow_token_generation: true })).resolves.toEqual({
      kind: "candidate",
      candidate: {
        access_token: "system-access-token-123456",
        platform_user_id: 42,
        identity_label: "alice",
      },
    });
    expect(fetchProfile).toHaveBeenCalledTimes(1);
  });

  it("does not generate when the logged-in profile cannot be verified", async () => {
    vi.stubGlobal("localStorage", storageWith({
      user: JSON.stringify({ id: 42, username: "alice", token: "browser-session-token" }),
    }));
    const fetchNewAPI = vi.fn(async (_input: URL | RequestInfo) => jsonResponse(401));
    vi.stubGlobal("fetch", fetchNewAPI);

    await expect(extractNewAPICredentials({ allow_token_generation: true })).resolves.toEqual({
      kind: "not_logged_in",
      message: "站点登录尚未完成",
    });
    expect(fetchNewAPI.mock.calls.map(([input]) => new URL(String(input)).pathname)).toEqual([
      "/api/user/self",
      "/api/user/profile",
    ]);
  });

  it("rejects an invalid generation response without exposing response data", async () => {
    vi.stubGlobal("localStorage", storageWith({
      user: JSON.stringify({ id: 42, username: "alice" }),
    }));
    vi.stubGlobal("fetch", vi.fn(async (input: URL | RequestInfo) => {
      const path = new URL(String(input)).pathname;
      return path === "/api/user/token"
        ? jsonResponse(200, { success: false, data: "unexpected-sensitive-data" })
        : jsonResponse(200, { data: { id: 42, username: "alice" } });
    }));

    const result = await extractNewAPICredentials({ allow_token_generation: true });

    expect(result).toEqual({ kind: "error", message: "系统访问令牌生成失败或响应格式无效" });
    if (result.kind !== "error") throw new Error("expected NewAPI token generation to fail");
    expect(result.message).not.toContain("unexpected-sensitive-data");
  });
});
