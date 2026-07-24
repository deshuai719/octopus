import type { ExtractResult } from "../types";

const ACCESS_TOKEN_KEYS = ["access_token", "auth_token", "sub2api_access_token", "token"] as const;
const REFRESH_TOKEN_KEYS = ["refresh_token", "sub2api_refresh_token"] as const;
const EXPIRES_KEYS = ["token_expires_at", "expires_at"] as const;
const PROFILE_PATHS = ["/api/v1/auth/me", "/api/v1/profile", "/api/profile"] as const;

function readFromStores(keys: readonly string[]): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const store of [localStorage, sessionStorage]) {
    for (const key of keys) {
      const raw = store.getItem(key)?.trim();
      if (!raw) continue;
      const value = raw.replace(/^Bearer\s+/i, "").trim();
      if (!value || seen.has(value)) continue;
      // Ignore obvious JSON blobs that are not bare tokens.
      if (value.startsWith("{") || value.startsWith("[")) continue;
      seen.add(value);
      out.push(value);
    }
  }
  return out;
}

function readFirst(keys: readonly string[]): string | undefined {
  return readFromStores(keys)[0];
}

function parseExpiry(raw: string | undefined): number | undefined {
  if (!raw || !/^\d+$/.test(raw)) return undefined;
  const parsed = Number(raw);
  return parsed < 1_000_000_000_000 ? parsed * 1000 : parsed;
}

async function profileWithBearer(accessToken: string): Promise<boolean> {
  for (const path of PROFILE_PATHS) {
    try {
      const response = await fetch(new URL(path, location.origin), {
        // Must succeed without browser cookies: Octopus server only has the bearer token.
        credentials: "omit",
        headers: {
          Accept: "application/json",
          Authorization: `Bearer ${accessToken}`,
        },
      });
      if (!response.ok) continue;
      const payload = await response.json().catch(() => null);
      if (!payload || typeof payload !== "object") continue;
      const data = (payload as { data?: unknown }).data;
      const record = data && typeof data === "object" ? (data as Record<string, unknown>) : (payload as Record<string, unknown>);
      const user = record.user && typeof record.user === "object" ? (record.user as Record<string, unknown>) : undefined;
      const hasIdentity = [record, user].some((item) =>
        !!item && (typeof item.id === "number" || typeof item.username === "string" || typeof item.email === "string"),
      );
      if (hasIdentity) return true;
    } catch {
      // try next path
    }
  }
  return false;
}

export async function extractSub2APICredentials(): Promise<ExtractResult> {
  const accessCandidates = readFromStores(ACCESS_TOKEN_KEYS);
  if (accessCandidates.length === 0) {
    return { kind: "not_logged_in", message: "没有找到 Sub2API access token，请先完成站点登录" };
  }

  // Prefer tokens that independently authenticate without session cookies.
  for (const accessToken of accessCandidates) {
    if (await profileWithBearer(accessToken)) {
      return {
        kind: "candidate",
        candidate: {
          access_token: accessToken,
          refresh_token: readFirst(REFRESH_TOKEN_KEYS),
          token_expires_at: parseExpiry(readFirst(EXPIRES_KEYS)),
        },
      };
    }
  }

  // Cookie session may keep the page logged in while stored bearer tokens are stale.
  try {
    const cookieProbe = await fetch(new URL("/api/v1/auth/me", location.origin), {
      credentials: "include",
      headers: { Accept: "application/json" },
    });
    if (cookieProbe.ok) {
      return {
        kind: "error",
        message: "站点当前主要靠 Cookie 会话登录，本地 access token 无法单独通过校验。请在站点重新登录或打开控制台复制有效的 access token，再使用扩展手动粘贴继续。",
      };
    }
  } catch {
    // ignore
  }

  return {
    kind: "error",
    message: "本地 access token 未能通过站点 /api/v1/auth/me 校验（401）。请重新登录站点后再试，或手动粘贴有效 access token。",
  };
}
