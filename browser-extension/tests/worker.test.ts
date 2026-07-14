import { beforeEach, describe, expect, it, vi } from "vitest";
import type { RecoveryPacket } from "../src/types";

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

let actionHandler: ((tab: chrome.tabs.Tab) => unknown) | undefined;
let messageHandler:
  | ((request: unknown, sender: chrome.runtime.MessageSender, sendResponse: (response: unknown) => void) => boolean)
  | undefined;
let resolveScript: ((value: Array<{ result: RecoveryPacket }>) => void) | undefined;
let openPanel: ReturnType<typeof vi.fn>;
let sendMessage: ReturnType<typeof vi.fn>;
let queryTabs: ReturnType<typeof vi.fn<() => Promise<chrome.tabs.Tab[]>>>;

beforeEach(() => {
  vi.resetModules();
  actionHandler = undefined;
  messageHandler = undefined;
  openPanel = vi.fn(async () => undefined);
  sendMessage = vi.fn(async () => undefined);
  queryTabs = vi.fn(async () => []);
  const scriptResult = new Promise<Array<{ result: RecoveryPacket }>>((resolve) => {
    resolveScript = resolve;
  });
  const stored: Record<string, unknown> = {};
  vi.stubGlobal("chrome", {
    action: { onClicked: { addListener: vi.fn((handler) => { actionHandler = handler; }) } },
    scripting: { executeScript: vi.fn(() => scriptResult) },
    sidePanel: { open: openPanel },
    storage: {
      session: {
        get: vi.fn(async (key: string) => ({ [key]: stored[key] })),
        set: vi.fn(async (value: Record<string, unknown>) => Object.assign(stored, value)),
        remove: vi.fn(async (key: string) => { delete stored[key]; }),
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
      sendMessage,
    },
    tabs: {
      query: queryTabs,
      create: vi.fn(async () => undefined),
      update: vi.fn(async () => undefined),
    },
    cookies: { get: vi.fn(async () => null) },
  });
});

describe("toolbar entry", () => {
  it("opens the side panel before waiting for recovery packet extraction", async () => {
    await import("../src/worker");

    expect(actionHandler).toBeTypeOf("function");
    actionHandler!({ id: 5, windowId: 7 } as chrome.tabs.Tab);

    expect(openPanel).toHaveBeenCalledWith({ windowId: 7 });
    expect(sendMessage).not.toHaveBeenCalled();

    resolveScript!([{ result: packet }]);
    await vi.waitFor(() => {
      expect(sendMessage).toHaveBeenCalledWith({ type: "session_updated", packet });
    });
  });

  it("rejects side-panel capture when the active tab changed after permission was granted", async () => {
    await import("../src/worker");
    resolveScript!([{ result: packet }]);
    queryTabs.mockResolvedValue([
      { id: 9, url: "https://other.example.com/" } as chrome.tabs.Tab,
    ]);

    const response = await new Promise<unknown>((resolve) => {
      messageHandler!(
        { type: "capture_active_session", expected_origin: "https://octopus.example.com" },
        {} as chrome.runtime.MessageSender,
        resolve,
      );
    });

    expect(response).toEqual({
      ok: false,
      message: "当前标签页已切换，请重新点击读取",
    });
    expect(chrome.scripting.executeScript).not.toHaveBeenCalled();
  });
});
