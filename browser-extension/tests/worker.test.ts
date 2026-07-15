import { beforeEach, describe, expect, it, vi } from "vitest";

let actionHandler: ((tab: chrome.tabs.Tab) => unknown) | undefined;
let messageHandler:
  | ((request: unknown, sender: chrome.runtime.MessageSender, sendResponse: (response: unknown) => void) => boolean)
  | undefined;
let openPanel: ReturnType<typeof vi.fn>;
let executeScript: ReturnType<typeof vi.fn>;
let queryTabs: ReturnType<typeof vi.fn<() => Promise<chrome.tabs.Tab[]>>>;

beforeEach(() => {
  vi.resetModules();
  actionHandler = undefined;
  messageHandler = undefined;
  openPanel = vi.fn(async () => undefined);
  executeScript = vi.fn(() => new Promise(() => undefined));
  queryTabs = vi.fn(async () => []);
  const sessionValues: Record<string, unknown> = {};
  vi.stubGlobal("chrome", {
    action: { onClicked: { addListener: vi.fn((handler) => { actionHandler = handler; }) } },
    scripting: { executeScript },
    sidePanel: { open: openPanel },
    storage: {
      local: {
        get: vi.fn(async () => ({})),
        set: vi.fn(async () => undefined),
        remove: vi.fn(async () => undefined),
      },
      session: {
        get: vi.fn(async (key: string) => ({ [key]: sessionValues[key] })),
        set: vi.fn(async (value: Record<string, unknown>) => Object.assign(sessionValues, value)),
        remove: vi.fn(async (keys: string | string[]) => {
          for (const key of Array.isArray(keys) ? keys : [keys]) delete sessionValues[key];
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
      query: queryTabs,
      create: vi.fn(async () => undefined),
      update: vi.fn(async () => undefined),
    },
    cookies: { get: vi.fn(async () => null) },
  });
});

describe("toolbar entry", () => {
  it("opens the side panel before reading the current page", async () => {
    await import("../src/worker");

    expect(actionHandler).toBeTypeOf("function");
    actionHandler!({ id: 5, windowId: 7, url: "https://octopus.example/settings" } as chrome.tabs.Tab);

    expect(openPanel).toHaveBeenCalledWith({ windowId: 7 });
    await vi.waitFor(() => expect(executeScript).toHaveBeenCalledTimes(1));
  });

  it("rejects side-panel capture when the active tab changed after permission was granted", async () => {
    await import("../src/worker");
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
    expect(executeScript).not.toHaveBeenCalled();
    expect(chrome.permissions.remove).toHaveBeenCalledWith({
      origins: ["https://octopus.example.com/*"],
    });
  });
});
