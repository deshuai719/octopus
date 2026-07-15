import { afterEach, describe, expect, it, vi } from "vitest";

import { clearLegacyRecoveryState } from "../src/legacy-cleanup";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("legacy recovery upgrade cleanup", () => {
  it("removes only legacy session state and its temporary permission", async () => {
    const binding = { version: 1, origin: "https://octopus.example", token: "secret" };
    const directIndex = { "https://relay.example": { operation_id: "direct" } };
    const diagnostics = [{ error_code: "safe" }];
    const localValues: Record<string, unknown> = {
      octopusBindingV1: binding,
      octopusDiagnosticsV1: diagnostics,
    };
    const sessionValues: Record<string, unknown> = {
      octopusRecoverySession: { origin: "https://legacy.example" },
      octopusRecoveryError: "old error",
      octopusNewAPITokenGenerationSession: "old-session",
      octopusDirectCaptureIndexV1: directIndex,
    };
    vi.stubGlobal("chrome", {
      storage: {
        local: { get: vi.fn(async (key: string) => ({ [key]: localValues[key] })) },
        session: {
          get: vi.fn(async (key: string) => ({ [key]: sessionValues[key] })),
          remove: vi.fn(async (keys: string[]) => keys.forEach((key) => { delete sessionValues[key]; })),
        },
      },
      alarms: { clear: vi.fn(async () => true) },
      permissions: { remove: vi.fn(async () => true) },
    });

    await clearLegacyRecoveryState();

    expect(sessionValues).toEqual({ octopusDirectCaptureIndexV1: directIndex });
    expect(localValues).toEqual({ octopusBindingV1: binding, octopusDiagnosticsV1: diagnostics });
    expect(chrome.alarms.clear).toHaveBeenCalledWith("octopusRecoveryExpiry");
    expect(chrome.permissions.remove).toHaveBeenCalledWith({ origins: ["https://legacy.example/*"] });
  });

  it("is idempotent when no legacy state exists", async () => {
    vi.stubGlobal("chrome", {
      storage: {
        local: { get: vi.fn(async () => ({})) },
        session: {
          get: vi.fn(async () => ({})),
          remove: vi.fn(async () => undefined),
        },
      },
      alarms: { clear: vi.fn(async () => false) },
      permissions: { remove: vi.fn(async () => true) },
    });

    await expect(clearLegacyRecoveryState()).resolves.toBeUndefined();
    expect(chrome.permissions.remove).not.toHaveBeenCalled();
  });

  it("keeps the host permission when a legacy target is the current binding", async () => {
    const origin = "https://octopus.example";
    vi.stubGlobal("chrome", {
      storage: {
        local: {
          get: vi.fn(async (key: string) => ({
            [key]: { version: 1, origin, token: "administrator", expire_at: "2099-01-01T00:00:00Z", validated_at: "2026-01-01T00:00:00Z" },
          })),
        },
        session: {
          get: vi.fn(async (key: string) => ({ [key]: { origin } })),
          remove: vi.fn(async () => undefined),
        },
      },
      alarms: { clear: vi.fn(async () => true) },
      permissions: { remove: vi.fn(async () => true) },
    });

    await clearLegacyRecoveryState();

    expect(chrome.permissions.remove).not.toHaveBeenCalled();
  });
});
