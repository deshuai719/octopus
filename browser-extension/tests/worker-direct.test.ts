import { beforeEach, describe, expect, it, vi } from "vitest";

import { OCTOPUS_BINDING_KEY } from "../src/binding";
import type { DirectCaptureView, OctopusBinding, WorkerResponse } from "../src/types";

let actionHandler: ((tab: chrome.tabs.Tab) => unknown) | undefined;
let messageHandler:
  | ((request: unknown, sender: chrome.runtime.MessageSender, sendResponse: (response: WorkerResponse) => void) => boolean)
  | undefined;
let executeScript: ReturnType<typeof vi.fn>;
let fetchMock: ReturnType<typeof vi.fn>;
let localValues: Record<string, unknown>;
let sessionValues: Record<string, unknown>;
let queryTabs: ReturnType<typeof vi.fn<() => Promise<chrome.tabs.Tab[]>>>;

function responseAt(url: string, body: string, init: ResponseInit = {}) {
  const response = new Response(body, init);
  Object.defineProperty(response, "url", { value: url });
  return response;
}

beforeEach(() => {
  vi.resetModules();
  actionHandler = undefined;
  messageHandler = undefined;
  const binding: OctopusBinding = {
    version: 1,
    origin: "https://octopus.example",
    token: "administrator-jwt-test-value",
    expire_at: "2099-01-01T00:00:00Z",
    validated_at: "2026-01-01T00:00:00Z",
  };
  localValues = { [OCTOPUS_BINDING_KEY]: binding };
  sessionValues = {};
  queryTabs = vi.fn(async () => []);
  executeScript = vi.fn()
    .mockResolvedValueOnce([{ result: { kind: "missing" } }])
    .mockResolvedValueOnce([{ result: undefined }])
    .mockResolvedValueOnce([{ result: {
      platform: "new-api",
      evidence: [{ code: "browser.strong.status_schema.new-api" }],
    } }])
    .mockResolvedValueOnce([{ result: {
      kind: "candidate",
      candidate: { access_token: "candidate-secret-test-value", platform_user_id: 42, identity_label: "user-42" },
    } }]);
  const capture: DirectCaptureView = {
    capture_id: "capture-id",
    operation_id: "550e8400-e29b-41d4-a716-446655440000",
    origin: "https://relay.example",
    platform: "new-api",
    phase: "preview_ready",
    expires_at: "2099-01-01T00:00:00Z",
    preview_version: "preview-version",
    candidate: { credential_type: "access_token", access_token_mask: "cand••••alue", has_refresh_token: false, platform_user_id: 42 },
  };
  fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.endsWith("/api/v1/user/status")) {
      return responseAt(url, JSON.stringify({ data: "ok" }), {
        status: 200,
        headers: { "X-Octopus-Direct-Capture-Version": "1" },
      });
    }
    return responseAt(url, JSON.stringify({ data: capture }), { status: 200 });
  });
  vi.stubGlobal("fetch", fetchMock);
  vi.stubGlobal("chrome", {
    action: { onClicked: { addListener: vi.fn((handler) => { actionHandler = handler; }) } },
    scripting: { executeScript },
    sidePanel: { open: vi.fn(async () => undefined) },
    storage: {
      local: {
        get: vi.fn(async (key: string) => ({ [key]: localValues[key] })),
        set: vi.fn(async (value: Record<string, unknown>) => Object.assign(localValues, value)),
        remove: vi.fn(async (key: string) => { delete localValues[key]; }),
      },
      session: {
        get: vi.fn(async (key: string) => ({ [key]: sessionValues[key] })),
        set: vi.fn(async (value: Record<string, unknown>) => Object.assign(sessionValues, value)),
        remove: vi.fn(async (key: string) => { delete sessionValues[key]; }),
      },
    },
    alarms: { create: vi.fn(), clear: vi.fn(), onAlarm: { addListener: vi.fn() } },
    permissions: {
      request: vi.fn(async () => true),
      contains: vi.fn(async () => true),
      remove: vi.fn(async () => true),
    },
    runtime: {
      onMessage: { addListener: vi.fn((handler) => { messageHandler = handler; }) },
      sendMessage: vi.fn(async () => undefined),
    },
    tabs: { query: queryTabs, create: vi.fn(), update: vi.fn() },
    cookies: { get: vi.fn(async () => null) },
  });
});

describe("direct capture worker flow", () => {
  it("routes a side-panel page read through Octopus binding and clears a stale recovery error", async () => {
    sessionValues.octopusRecoveryError = "恢复会话格式无效";
    executeScript.mockReset()
      .mockResolvedValueOnce([{ result: { kind: "missing" } }])
      .mockResolvedValueOnce([{ result: {
        token: "fresh-administrator-jwt",
        expire_at: "2099-01-01T00:00:00Z",
        is_api_key_auth: false,
      } }]);
    queryTabs.mockResolvedValue([
      { id: 8, windowId: 3, url: "https://octopus.example/settings" } as chrome.tabs.Tab,
    ]);
    await import("../src/worker");

    const response = await new Promise<WorkerResponse>((resolve) => {
      messageHandler!(
        { type: "capture_active_session", expected_origin: "https://octopus.example" },
        {} as chrome.runtime.MessageSender,
        resolve,
      );
    });

    expect(response).toMatchObject({ ok: true, mode: "binding", origin: "https://octopus.example" });
    expect(sessionValues.octopusRecoveryError).toBeUndefined();
    expect(chrome.permissions.remove).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("reports malformed recovery JSON instead of falling through to direct capture", async () => {
    executeScript.mockReset().mockResolvedValueOnce([{ result: { kind: "invalid_json" } }]);
    queryTabs.mockResolvedValue([
      { id: 7, windowId: 3, url: "https://relay.example/console" } as chrome.tabs.Tab,
    ]);
    await import("../src/worker");

    const response = await new Promise<WorkerResponse>((resolve) => {
      messageHandler!(
        { type: "capture_active_session", expected_origin: "https://relay.example" },
        {} as chrome.runtime.MessageSender,
        resolve,
      );
    });

    expect(response).toEqual({
      ok: false,
      origin: "https://relay.example",
      message: "恢复会话 JSON 无效",
    });
    expect(fetchMock).not.toHaveBeenCalled();
    expect(chrome.permissions.remove).toHaveBeenCalledWith({
      origins: ["https://relay.example/*"],
    });
  });

  it("routes a side-panel page read through direct capture when no recovery packet exists", async () => {
    queryTabs.mockResolvedValue([
      { id: 7, windowId: 3, url: "https://relay.example/console" } as chrome.tabs.Tab,
    ]);
    await import("../src/worker");

    const response = await new Promise<WorkerResponse>((resolve) => {
      messageHandler!(
        { type: "capture_active_session", expected_origin: "https://relay.example" },
        {} as chrome.runtime.MessageSender,
        resolve,
      );
    });

    expect(response).toMatchObject({
      ok: true,
      mode: "direct",
      origin: "https://relay.example",
      capture: { phase: "preview_ready" },
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(chrome.permissions.remove).toHaveBeenCalledWith({
      origins: ["https://relay.example/*"],
    });
  });

  it("submits structural evidence and never persists the raw candidate", async () => {
    await import("../src/worker");
    actionHandler!({ id: 7, windowId: 3, url: "https://relay.example/console" } as chrome.tabs.Tab);

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    const previewCall = fetchMock.mock.calls.find(([input]) => String(input).endsWith("/api/v1/site/direct-capture/preview"));
    expect(previewCall).toBeDefined();
    const body = JSON.parse(String(previewCall?.[1]?.body)) as Record<string, unknown>;
    expect(body).toMatchObject({
      origin: "https://relay.example",
      platform: "new-api",
      access_token: "candidate-secret-test-value",
      platform_user_id: 42,
      evidence: [{ code: "browser.strong.status_schema.new-api" }],
    });
    expect(JSON.stringify(sessionValues)).not.toContain("candidate-secret-test-value");
    expect(messageHandler).toBeTypeOf("function");
  });
});
