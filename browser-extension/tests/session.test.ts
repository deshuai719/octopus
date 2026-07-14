import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  claimNewAPITokenGeneration,
  discardRecoverySession,
  getRecoverySession,
  NEWAPI_TOKEN_GENERATION_SESSION_KEY,
  RECOVERY_ERROR_KEY,
  RECOVERY_EXPIRY_ALARM,
  RECOVERY_SESSION_KEY,
  storeRecoverySession,
} from "../src/session";
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

let stored: Record<string, unknown>;
let removePermission: ReturnType<typeof vi.fn>;
let clearAlarm: ReturnType<typeof vi.fn>;

beforeEach(() => {
  stored = {};
  removePermission = vi.fn().mockResolvedValue(true);
  clearAlarm = vi.fn().mockResolvedValue(true);
  vi.stubGlobal("chrome", {
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
      create: vi.fn().mockResolvedValue(undefined),
      clear: clearAlarm,
    },
    permissions: { remove: removePermission },
  });
});

describe("recovery session lifecycle", () => {
  it("stores the capability only in session storage and schedules expiry", async () => {
    await storeRecoverySession(packet);

    expect(stored[RECOVERY_SESSION_KEY]).toEqual(packet);
    expect(chrome.alarms.create).toHaveBeenCalledWith(RECOVERY_EXPIRY_ALARM, {
      when: Date.parse(packet.expires_at),
    });
    expect(await getRecoverySession()).toEqual(packet);
  });

  it("clears session data, expiry alarm, and the exact origin permission", async () => {
    stored[RECOVERY_SESSION_KEY] = packet;
    stored[RECOVERY_ERROR_KEY] = "恢复会话版本不受支持";
    stored[NEWAPI_TOKEN_GENERATION_SESSION_KEY] = packet.session_id;

    await discardRecoverySession();

    expect(stored[RECOVERY_SESSION_KEY]).toBeUndefined();
    expect(stored[RECOVERY_ERROR_KEY]).toBeUndefined();
    expect(stored[NEWAPI_TOKEN_GENERATION_SESSION_KEY]).toBeUndefined();
    expect(clearAlarm).toHaveBeenCalledWith(RECOVERY_EXPIRY_ALARM);
    expect(removePermission).toHaveBeenCalledWith({ origins: ["https://site.example.com/*"] });
    expect(await getRecoverySession()).toBeUndefined();
  });

  it("allows only one concurrent NewAPI token-generation claim per recovery session", async () => {
    const claims = await Promise.all([
      claimNewAPITokenGeneration(packet.session_id),
      claimNewAPITokenGeneration(packet.session_id),
    ]);

    expect(claims.sort()).toEqual([false, true]);
    expect(stored[NEWAPI_TOKEN_GENERATION_SESSION_KEY]).toBe(packet.session_id);
  });

  it("honors a persisted generation marker after a service-worker restart", async () => {
    const restartedSessionID = `${packet.session_id}-after-restart`;
    stored[NEWAPI_TOKEN_GENERATION_SESSION_KEY] = restartedSessionID;

    expect(await claimNewAPITokenGeneration(restartedSessionID)).toBe(false);
  });

  it("destroys an expired packet instead of returning its capability", async () => {
    stored[RECOVERY_SESSION_KEY] = {
      ...packet,
      expires_at: new Date(Date.now() - 1_000).toISOString(),
    };

    expect(await getRecoverySession()).toBeUndefined();
    expect(stored[RECOVERY_SESSION_KEY]).toBeUndefined();
    expect(removePermission).toHaveBeenCalledWith({ origins: ["https://site.example.com/*"] });
  });
});
