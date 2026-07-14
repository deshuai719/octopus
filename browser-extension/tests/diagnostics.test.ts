import { afterEach, describe, expect, it, vi } from "vitest";

import { clearDiagnostics, formatDiagnostics, listDiagnostics, recordDiagnostic, sanitizeDiagnosticMessage } from "../src/diagnostics";

function installStorage() {
  const values: Record<string, unknown> = {};
  const local = {
    get: vi.fn(async (key: string) => ({ [key]: values[key] })),
    set: vi.fn(async (items: Record<string, unknown>) => Object.assign(values, items)),
    remove: vi.fn(async (key: string) => { delete values[key]; }),
  };
  vi.stubGlobal("chrome", { storage: { local } });
  return { values, local };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("diagnostics", () => {
  it("redacts secrets and exports only the diagnostic contract", async () => {
    installStorage();
    await recordDiagnostic({
      at: new Date().toISOString(),
      operation_id: "00000000-0000-4000-8000-000000000001",
      origin: "https://relay.example",
      error_code: "extension.capture.failed",
      stage: "credential_extraction",
      message: "Authorization: Bearer abc.def and token=plain-secret",
    });

    const items = await listDiagnostics();
    expect(items).toHaveLength(1);
    const exported = formatDiagnostics(items);
    expect(exported).toContain("Bearer [REDACTED]");
    expect(exported).toContain("token=[REDACTED]");
    expect(exported).not.toContain("abc.def");
    expect(exported).not.toContain("plain-secret");
  });

  it("keeps at most twenty recent entries and supports clearing", async () => {
    const { local } = installStorage();
    for (let index = 0; index < 22; index += 1) {
      await recordDiagnostic({
        at: new Date(Date.now() + index).toISOString(),
        operation_id: String(index),
        error_code: "test",
        stage: "binding",
        message: "safe",
      });
    }
    expect(await listDiagnostics()).toHaveLength(20);
    await clearDiagnostics();
    expect(local.remove).toHaveBeenCalledOnce();
    expect(await listDiagnostics()).toEqual([]);
  });

  it("limits arbitrary error messages", () => {
    expect(sanitizeDiagnosticMessage("x".repeat(700))).toHaveLength(512);
  });
});
