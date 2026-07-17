import { describe, expect, it } from "vitest";
import { checkIsFresh, compareExtensionVersions, manualInstallerPending, sha256Hex } from "../src/updater";

describe("extension updater state", () => {
  it("compares Chrome manifest versions numerically", () => {
    expect(compareExtensionVersions("0.2.0", "0.3.0")).toBe(-1);
    expect(compareExtensionVersions("1.2", "1.2.0")).toBe(0);
    expect(compareExtensionVersions("2.0.0", "1.99.99")).toBe(1);
    expect(() => compareExtensionVersions("1.beta", "1.0.0")).toThrow();
  });

  it("uses a six-hour successful-check TTL", () => {
    const now = Date.parse("2026-07-17T12:00:00Z");
    const state = {
      version: 1 as const,
      phase: "ready" as const,
      current_version: "0.3.0",
      update_available: false,
      targets: [],
      message: "ok",
      checked_at: "2026-07-17T07:00:00Z",
    };
    expect(checkIsFresh(state, now)).toBe(true);
    expect(checkIsFresh({ ...state, checked_at: "2026-07-17T05:00:00Z" }, now)).toBe(false);
  });

  it("preserves a downloaded helper until the user runs it manually", () => {
    const base = {
      version: 1 as const,
      current_version: "0.3.1",
      update_available: false,
      targets: [],
      message: "manual",
    };
    expect(manualInstallerPending({ ...base, phase: "installer_ready", installer_download_id: 42 })).toBe(true);
    expect(manualInstallerPending({ ...base, phase: "installing", installer_download_id: 42 })).toBe(true);
    expect(manualInstallerPending({ ...base, phase: "installer_ready" })).toBe(false);
    expect(manualInstallerPending({ ...base, phase: "host_required", installer_download_id: 42 })).toBe(false);
  });

  it("computes a deterministic SHA-256 for the bundled updater", async () => {
    const payload = new TextEncoder().encode("octopus").buffer;
    expect(await sha256Hex(payload)).toBe("5633c9b8af6d089859afcbec42fdc03f8c407aaba9668218b433bd4959911465");
  });
});
