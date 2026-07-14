import type { Platform, RecoveryPacket } from "./types";

const SUPPORTED_PLATFORMS = new Set<Platform>([
  "new-api",
  "one-api",
  "one-hub",
  "done-hub",
  "sub2api",
  "anyrouter",
]);

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function httpOriginFromURL(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  try {
    const parsed = new URL(value);
    if (parsed.protocol !== "https:" && parsed.protocol !== "http:") return undefined;
    return parsed.origin;
  } catch {
    return undefined;
  }
}

function validHTTPOrigin(value: unknown) {
  return httpOriginFromURL(value) === value;
}

export function parseRecoveryPacket(value: unknown): RecoveryPacket {
  if (!isRecord(value)) throw new Error("恢复会话格式无效");
  const version = "version" in value ? value.version : 1;
  if (version !== 1) throw new Error("恢复会话版本不受支持");
  if (!validHTTPOrigin(value.api_base_url) || !validHTTPOrigin(value.origin)) {
    throw new Error("恢复会话 origin 无效");
  }
  if (
    typeof value.session_id !== "string" ||
    value.session_id.length < 16 ||
    typeof value.capability !== "string" ||
    value.capability.length < 32 ||
    typeof value.account_id !== "number" ||
    value.account_id <= 0 ||
    typeof value.site_id !== "number" ||
    value.site_id <= 0 ||
    typeof value.platform !== "string" ||
    !SUPPORTED_PLATFORMS.has(value.platform as Platform) ||
    typeof value.expires_at !== "string" ||
    !isRecord(value.auth) ||
    typeof value.auth.compatible_family !== "string" ||
    !Array.isArray(value.auth.required_fields) ||
    !Array.isArray(value.auth.extractable_fields) ||
    !isRecord(value.auth.recovery_guide) ||
    typeof value.auth.recovery_guide.title !== "string" ||
    !Array.isArray(value.auth.recovery_guide.steps) ||
    typeof value.auth.recovery_guide.manual_fallback !== "string"
  ) {
    throw new Error("恢复会话字段不完整");
  }
  if (Date.parse(value.expires_at) <= Date.now()) throw new Error("恢复会话已过期");
  return { ...value, version: 1 } as unknown as RecoveryPacket;
}

export function permissionPattern(origin: string) {
  return `${new URL(origin).origin}/*`;
}
