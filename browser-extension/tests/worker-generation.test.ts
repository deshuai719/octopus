import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  NEWAPI_TOKEN_GENERATION_SESSION_KEY,
  RECOVERY_SESSION_KEY,
} from "../src/session";
import type { ExtractResult, RecoveryPacket, WorkerResponse } from "../src/types";

const packet: RecoveryPacket = {
  version: 1,
  api_base_url: "https://octopus.example.com",
  session_id: "session-id-long-enough",
  capability: "capability-value-that-is-long-enough-123",
  account_id: 1,
  site_id: 2,
  origin: "https://site.example.com",
  platform: "new-api",
  expires_at: new Date(Date.now() + 60_000).toISOString(),
  auth: {
    compatible_family: "new-api",
    required_fields: ["access_token", "platform_user_id"],
    extractable_fields: ["access_token", "platform_user_id"],
    recovery_guide: { title: "重新登录", steps: ["登录"], manual_fallback: "手动粘贴" },
  },
};

let stored: Record<string, unknown>;
let messageHandler:
  | ((request: unknown, sender: chrome.runtime.MessageSender, sendResponse: (response: WorkerResponse) => void) => boolean)
  | undefined;
let executeScript: ReturnType<typeof vi.fn>;
let submitCandidate: ReturnType<typeof vi.fn>;

function missingToken(): ExtractResult {
  return {
    kind: "manual_required",
    reason: "system_token_missing",
    message: "没有完整系统访问令牌",
  };
}

function generatedToken(): ExtractResult {
  return {
    kind: "candidate",
    generated_system_token: true,
    candidate: {
      access_token: "generated-system-token",
      platform_user_id: 42,
    },
  };
}

async function sendExtract(): Promise<WorkerResponse> {
  return new Promise((resolve) => {
    messageHandler!(
      { type: "extract_and_submit" },
      {} as chrome.runtime.MessageSender,
      resolve,
    );
  });
}

beforeEach(() => {
  vi.resetModules();
  stored = { [RECOVERY_SESSION_KEY]: packet };
  messageHandler = undefined;
  executeScript = vi.fn()
    .mockResolvedValueOnce([{ result: missingToken() }])
    .mockResolvedValueOnce([{ result: generatedToken() }]);
  submitCandidate = vi.fn(async () => ({
    ok: true,
    status: 200,
    json: async () => ({}),
  }));
  vi.stubGlobal("fetch", submitCandidate);
  vi.stubGlobal("chrome", {
    action: { onClicked: { addListener: vi.fn() } },
    scripting: { executeScript },
    sidePanel: { open: vi.fn(async () => undefined) },
    storage: {
      session: {
        get: vi.fn(async (key: string) => ({ [key]: stored[key] })),
        set: vi.fn(async (value: Record<string, unknown>) => Object.assign(stored, value)),
        remove: vi.fn(async (keys: string | string[]) => {
          for (const key of Array.isArray(keys) ? keys : [keys]) delete stored[key];
        }),
      },
    },
    alarms: {
      create: vi.fn(async () => undefined),
      clear: vi.fn(async () => true),
      onAlarm: { addListener: vi.fn() },
    },
    permissions: {
      contains: vi.fn(async () => true),
      remove: vi.fn(async () => true),
    },
    runtime: {
      onMessage: { addListener: vi.fn((handler) => { messageHandler = handler; }) },
      sendMessage: vi.fn(async () => undefined),
    },
    tabs: {
      query: vi.fn(async () => [{ id: 9, active: true, url: `${packet.origin}/console` }]),
      create: vi.fn(async () => undefined),
      update: vi.fn(async () => undefined),
    },
    cookies: { get: vi.fn(async () => null) },
  });
});

describe("NewAPI automatic system-token generation", () => {
  it("does a read-only probe before claiming and allowing one generation", async () => {
    await import("../src/worker");

    const response = await sendExtract();

    expect(response).toMatchObject({ ok: true, result: { kind: "candidate" } });
    expect(executeScript).toHaveBeenCalledTimes(2);
    expect(executeScript.mock.calls[0][0]).toMatchObject({ args: [{ allow_token_generation: false }] });
    expect(executeScript.mock.calls[1][0]).toMatchObject({ args: [{ allow_token_generation: true }] });
    expect(submitCandidate).toHaveBeenCalledTimes(1);
    const submittedBody = JSON.parse(String(submitCandidate.mock.calls[0][1]?.body)) as Record<string, unknown>;
    expect(submittedBody).toEqual({
      account_id: packet.account_id,
      origin: packet.origin,
      platform: packet.platform,
      access_token: "generated-system-token",
      platform_user_id: 42,
    });
    expect(submittedBody).not.toHaveProperty("generated_system_token");
  });

  it("does not generate again when the recovery session already has a persisted marker", async () => {
    stored[NEWAPI_TOKEN_GENERATION_SESSION_KEY] = packet.session_id;
    await import("../src/worker");

    const response = await sendExtract();

    expect(response).toMatchObject({
      ok: false,
      result: {
        kind: "error",
        message: expect.stringContaining("本恢复会话已经尝试过自动生成"),
      },
    });
    expect(executeScript).toHaveBeenCalledTimes(1);
    expect(submitCandidate).not.toHaveBeenCalled();
  });

  it("reports that the NewAPI token changed when candidate submission fails", async () => {
    submitCandidate.mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({ error_code: "site.upstream.server_error" }),
    });
    await import("../src/worker");

    const response = await sendExtract();

    expect(response).toMatchObject({
      ok: false,
      result: {
        kind: "error",
        message: expect.stringContaining("系统令牌已更新，但尚未保存到 Octopus"),
      },
    });
  });
});
