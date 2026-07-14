import { describe, expect, it } from "vitest";
import { parseRecoveryPacket, permissionPattern } from "../src/packet";

const packet = {
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
} as const;

describe("parseRecoveryPacket", () => {
  it("accepts a bound, unexpired packet", () => {
    expect(parseRecoveryPacket(packet).origin).toBe(packet.origin);
  });

  it("normalizes a structurally valid legacy packet without a version", () => {
    const { version: _version, ...legacyPacket } = packet;

    expect(parseRecoveryPacket(legacyPacket)).toEqual(packet);
  });

  it("rejects an explicitly unsupported version", () => {
    expect(() => parseRecoveryPacket({ ...packet, version: 2 })).toThrow("恢复会话版本不受支持");
  });

  it("rejects unsupported platforms", () => {
    expect(() => parseRecoveryPacket({ ...packet, platform: "unknown" })).toThrow();
  });

  it("rejects URLs containing paths instead of exact origins", () => {
    expect(() => parseRecoveryPacket({ ...packet, origin: "https://site.example.com/login" })).toThrow();
  });

  it("builds an exact-origin optional permission", () => {
    expect(permissionPattern(packet.origin)).toBe("https://site.example.com/*");
  });
});
