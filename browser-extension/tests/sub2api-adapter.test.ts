import { beforeEach, describe, expect, it, vi } from "vitest";
import { extractSub2APICredentials } from "../src/adapters/sub2api";

function storageWith(values: Record<string, string>): Storage {
  return {
    getItem: (key: string) => values[key] ?? null,
  } as Storage;
}

describe("Sub2API credential extraction", () => {
  beforeEach(() => {
    vi.stubGlobal("localStorage", storageWith({}));
    vi.stubGlobal("sessionStorage", storageWith({}));
    vi.stubGlobal("location", { origin: "https://relay.example" });
    vi.stubGlobal("fetch", vi.fn());
  });

  it("reads a bearer token that independently passes auth/me without cookies", async () => {
    vi.stubGlobal("localStorage", storageWith({
      auth_token: "stale-token",
      access_token: "good-access-token",
      refresh_token: "sub2api-refresh-token",
      token_expires_at: "4102444800000",
    }));
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const auth = String((init?.headers as Record<string, string>)?.Authorization ?? "");
      if (url.endsWith("/api/v1/auth/me") && auth === "Bearer good-access-token") {
        return new Response(JSON.stringify({ code: 0, data: { id: 7, username: "user" } }), { status: 200 });
      }
      return new Response(JSON.stringify({ code: "INVALID_TOKEN" }), { status: 401 });
    });
    vi.stubGlobal("fetch", fetchMock);

    await expect(extractSub2APICredentials()).resolves.toEqual({
      kind: "candidate",
      candidate: {
        access_token: "good-access-token",
        refresh_token: "sub2api-refresh-token",
        token_expires_at: 4_102_444_800_000,
      },
    });
    expect(fetchMock).toHaveBeenCalled();
    const cookieMode = fetchMock.mock.calls.some((call) => call[1]?.credentials === "include");
    expect(cookieMode).toBe(false);
  });

  it("reports cookie-only sessions instead of sending a failing token", async () => {
    vi.stubGlobal("localStorage", storageWith({ auth_token: "stale-token" }));
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/api/v1/auth/me") && init?.credentials === "include") {
        return new Response(JSON.stringify({ code: 0, data: { id: 1, username: "cookie-user" } }), { status: 200 });
      }
      return new Response(JSON.stringify({ code: "INVALID_TOKEN" }), { status: 401 });
    });
    vi.stubGlobal("fetch", fetchMock);

    await expect(extractSub2APICredentials()).resolves.toMatchObject({
      kind: "error",
      message: expect.stringContaining("Cookie"),
    });
  });

  it("does not enumerate unrelated token-like storage keys", async () => {
    vi.stubGlobal("localStorage", storageWith({ api_token: "unapproved-token" }));
    await expect(extractSub2APICredentials()).resolves.toMatchObject({ kind: "not_logged_in" });
  });
});
