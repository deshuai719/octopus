import type { Platform, PlatformEvidence } from "./types";

export type PlatformDiscovery = {
  platform?: Platform;
  evidence: PlatformEvidence[];
  system_name?: string;
  reason?: "variant_inconclusive" | "not_logged_in";
};

export type PlatformStatusSignature = {
  platform: Extract<Platform, "new-api" | "one-api" | "one-hub" | "done-hub">;
  required_keys: string[];
  forbidden_keys?: string[];
};

// These are structural fields from the canonical upstream /api/status handlers.
// User-editable values such as system_name, title, logo, and domain are never decisive.
export const PLATFORM_STATUS_SIGNATURES: PlatformStatusSignature[] = [
  {
    platform: "new-api",
    required_keys: ["quota_display_type", "passkey_login", "setup"],
  },
  {
    platform: "done-hub",
    required_keys: ["linuxDo_oauth", "user_agreement_enabled", "max_log_query_days"],
  },
  {
    platform: "one-hub",
    required_keys: ["oidc_auth", "language", "EnableSafe", "UptimeDomain"],
    forbidden_keys: ["linuxDo_oauth", "max_log_query_days"],
  },
  {
    platform: "one-api",
    required_keys: ["oidc", "oidc_well_known", "oidc_token_endpoint"],
    forbidden_keys: ["oidc_auth", "quota_display_type"],
  },
];

export async function detectCurrentPlatform(signatures: PlatformStatusSignature[]): Promise<PlatformDiscovery> {
  // chrome.scripting.executeScript serializes func without module closures, so every
  // runtime helper used by this injected function must remain inside its body.
  const isRecord = (value: unknown): value is Record<string, unknown> =>
    !!value && typeof value === "object" && !Array.isArray(value);
  const dataRecord = (payload: unknown): Record<string, unknown> | undefined => {
    if (!isRecord(payload)) return undefined;
    return isRecord(payload.data) ? payload.data : payload;
  };
  const isDoneHubPartialStatus = (status: Record<string, unknown>): boolean =>
    Object.hasOwn(status, "linuxDo_oauth") && Object.hasOwn(status, "max_log_query_days");
  const isDoneHubGroupMap = (payload: unknown): boolean => {
    if (!isRecord(payload) || payload.success !== true || !isRecord(payload.data)) return false;
    const groups = Object.entries(payload.data);
    if (groups.length === 0) return false;
    return groups.every(([groupKey, rawGroup]) => {
      if (!groupKey.trim() || !isRecord(rawGroup)) return false;
      const name = typeof rawGroup.name === "string" ? rawGroup.name.trim() : "";
      const symbol = typeof rawGroup.symbol === "string" ? rawGroup.symbol.trim() : "";
      return !!name && !!symbol && (Object.hasOwn(rawGroup, "ratio") || Object.hasOwn(rawGroup, "dynamic_ratio"));
    });
  };
  const responseJSON = async (path: string, init: RequestInit = {}): Promise<unknown> => {
    try {
      const response = await fetch(new URL(path, location.origin), {
        credentials: "include",
        headers: { Accept: "application/json", ...(init.headers ?? {}) },
        ...init,
      });
      return response.ok ? await response.json() : undefined;
    } catch {
      return undefined;
    }
  };

  const status = await responseJSON("/api/status");
  const statusData = dataRecord(status);
  const systemName = statusData && typeof statusData.system_name === "string" ? statusData.system_name : undefined;
  if (statusData) {
    const matched = signatures.filter((signature) =>
      signature.required_keys.every((key) => Object.hasOwn(statusData, key)) &&
      !(signature.forbidden_keys ?? []).some((key) => Object.hasOwn(statusData, key)),
    );
    if (matched.length === 1) {
      const platform = matched[0].platform;
      return {
        platform,
        evidence: [{ code: `browser.strong.status_schema.${platform}` }],
        system_name: systemName,
      };
    }
    if (matched.length > 1) {
      return { evidence: matched.map((item) => ({ code: `browser.conflict.status_schema.${item.platform}` })), system_name: systemName, reason: "variant_inconclusive" };
    }
    if (isRecord(status) && status.success === true && isDoneHubPartialStatus(statusData)) {
      const groupMap = await responseJSON("/api/user_group_map", { credentials: "omit" });
      if (isDoneHubGroupMap(groupMap)) {
        return {
          platform: "done-hub",
          evidence: [{ code: "browser.strong.status_group_schema.done-hub" }],
          system_name: systemName,
        };
      }
    }
  }

  const authToken = localStorage.getItem("auth_token")?.trim();
  const refreshToken = localStorage.getItem("refresh_token")?.trim();
  if (authToken && refreshToken) {
    const profile = dataRecord(await responseJSON("/api/v1/profile", {
      headers: { Authorization: `Bearer ${authToken}` },
    }));
    if (profile && (typeof profile.id === "number" || typeof profile.username === "string" || typeof profile.email === "string")) {
      return {
        platform: "sub2api",
        evidence: [
          { code: "browser.medium.storage.sub2api_token_pair" },
          { code: "browser.medium.auth_profile.sub2api" },
        ],
        system_name: systemName,
      };
    }
  }

  const currentUser = dataRecord(await responseJSON("/api/user/self"));
  if (currentUser) {
    const id = typeof currentUser.id === "number" ? currentUser.id : Number(currentUser.id);
    const username = typeof currentUser.username === "string" ? currentUser.username : "";
    if (Number.isSafeInteger(id) && id > 0 && /^linuxdo[_-]\d+$/i.test(username)) {
      return {
        platform: "anyrouter",
        evidence: [
          { code: "browser.medium.auth_profile.anyrouter" },
          { code: "browser.medium.identity.linuxdo_numeric" },
        ],
        system_name: systemName,
      };
    }
  }

  const weakText = `${document.title}\n${systemName ?? ""}`.toLowerCase();
  const weakPatterns: Array<[Platform, RegExp]> = [
    ["done-hub", /\bdone[\s_-]*hub\b/],
    ["one-hub", /\bone[\s_-]*hub\b/],
    ["one-api", /\bone[\s_-]*api\b/],
    ["sub2api", /sub2api/],
    ["anyrouter", /anyrouter/],
    ["new-api", /new[\s_-]*api/],
  ];
  const weak = weakPatterns.filter(([, pattern]) => pattern.test(weakText));
  return {
    evidence: weak.map(([platform]) => ({ code: `browser.weak.brand.${platform}` })),
    system_name: systemName,
    reason: weak.length > 0 ? "variant_inconclusive" : "not_logged_in",
  };
}
