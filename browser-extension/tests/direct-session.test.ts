import { afterEach, describe, expect, it, vi } from "vitest";

import {
  claimDirectTokenGeneration,
  clearAllDirectSessions,
  getDirectSession,
  getDirectCaptureSummary,
  shouldRemoveDirectSession,
  putDirectSession,
  removeDirectSession,
} from "../src/direct-session";

function installSessionStorage() {
  const values: Record<string, unknown> = {};
  vi.stubGlobal("chrome", {
    storage: {
      session: {
        get: vi.fn(async (key: string) => ({ [key]: values[key] })),
        set: vi.fn(async (items: Record<string, unknown>) => Object.assign(values, items)),
        remove: vi.fn(async (key: string) => { delete values[key]; }),
      },
    },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("direct capture session index", () => {
  it("isolates sessions by canonical origin", async () => {
    installSessionStorage();
    const expires = new Date(Date.now() + 60_000).toISOString();
    await putDirectSession({ origin: "https://one.example", operation_id: "one", phase: "completed", expires_at: expires, generation_attempted: false });
    await putDirectSession({ origin: "https://two.example", operation_id: "two", phase: "preview_ready", expires_at: expires, generation_attempted: false });
    await putDirectSession({ origin: "https://three.example", operation_id: "three", phase: "credential_generation", expires_at: expires, generation_attempted: false });
    expect((await getDirectSession("https://one.example"))?.operation_id).toBe("one");
    expect((await getDirectSession("https://two.example"))?.operation_id).toBe("two");
    expect((await getDirectSession("https://three.example"))?.operation_id).toBe("three");
    await removeDirectSession("https://one.example");
    expect(await getDirectSession("https://one.example")).toBeUndefined();
    expect((await getDirectSession("https://two.example"))?.operation_id).toBe("two");
    expect((await getDirectSession("https://three.example"))?.operation_id).toBe("three");
  });

  it("allows system-token generation only once for the same operation", async () => {
    installSessionStorage();
    await putDirectSession({
      origin: "https://relay.example",
      operation_id: "operation",
      platform: "new-api",
      phase: "credential_generation",
      expires_at: new Date(Date.now() + 60_000).toISOString(),
      generation_attempted: false,
    });
    await expect(claimDirectTokenGeneration("https://relay.example", "operation")).resolves.toBe(true);
    await expect(claimDirectTokenGeneration("https://relay.example", "operation")).resolves.toBe(false);
    await expect(claimDirectTokenGeneration("https://relay.example", "different")).resolves.toBe(false);
  });

  it("drops expired sessions and clears all remaining indexes", async () => {
    installSessionStorage();
    await putDirectSession({ origin: "https://expired.example", operation_id: "expired", phase: "failed", expires_at: new Date(Date.now() - 1).toISOString(), generation_attempted: false });
    expect(await getDirectSession("https://expired.example")).toBeUndefined();
    await putDirectSession({ origin: "https://active.example", operation_id: "active", phase: "preview_ready", expires_at: new Date(Date.now() + 60_000).toISOString(), generation_attempted: false });
    const cleared = await clearAllDirectSessions();
    expect(cleared.map((item) => item.origin)).toEqual(["https://active.example"]);
    expect(await getDirectSession("https://active.example")).toBeUndefined();
  });

  it("keeps terminal results until their session TTL expires", () => {
    for (const phase of ["sync_failed", "completed", "canceled", "failed", "conflict", "expired"]) {
      expect(shouldRemoveDirectSession(phase)).toBe(false);
    }
  });

  it("preserves creation time, tracks sync start, and limits the safe summary", async () => {
    installSessionStorage();
    const expires = new Date(Date.now() + 60_000).toISOString();
    await putDirectSession({ origin: "https://active.example", operation_id: "active", capture_id: "capture-active", phase: "preview_ready", expires_at: expires, generation_attempted: false });
    const createdAt = (await getDirectSession("https://active.example"))?.created_at;
    await putDirectSession({ origin: "https://active.example", operation_id: "active", capture_id: "capture-active", phase: "saved_syncing", expires_at: expires, generation_attempted: false, capture: { capture_id: "capture-active", operation_id: "active", origin: "https://active.example", platform: "new-api", phase: "saved_syncing", expires_at: expires, saved: { action: "create_account", site_id: 1, account_id: 2 } } });
    expect((await getDirectSession("https://active.example"))?.created_at).toBe(createdAt);
    expect((await getDirectSession("https://active.example"))?.sync_started_at).toBeTruthy();
    for (let index = 0; index < 21; index += 1) {
      await putDirectSession({ origin: `https://done-${index}.example`, operation_id: `done-${index}`, phase: "completed", expires_at: expires, generation_attempted: false });
    }
    const summary = await getDirectCaptureSummary();
    expect(summary).toHaveLength(20);
    expect(summary[0]).toMatchObject({ origin: "https://active.example", phase: "saved_syncing", saved: true });
  });
});
