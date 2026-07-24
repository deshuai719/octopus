import type { ExtractResult } from "../types";

/**
 * IMPORTANT: chrome.scripting.executeScript serializes this function into the page
 * without module closures. Every helper and constant must stay inside the body.
 */
export async function extractSub2APICredentials(): Promise<ExtractResult> {
  const accessTokenKeys = ["access_token", "auth_token", "sub2api_access_token", "token"];
  const refreshTokenKeys = ["refresh_token", "sub2api_refresh_token"];
  const expiresKeys = ["token_expires_at", "expires_at"];
  const profilePaths = ["/api/v1/auth/me", "/api/v1/profile", "/api/profile"];

  const readFromStores = (keys: string[]): string[] => {
    const out: string[] = [];
    const seen = new Set<string>();
    for (const store of [localStorage, sessionStorage]) {
      for (const key of keys) {
        const raw = store.getItem(key)?.trim();
        if (!raw) continue;
        const value = raw.replace(/^Bearer\s+/i, "").trim();
        if (!value || seen.has(value)) continue;
        if (value.startsWith("{") || value.startsWith("[")) continue;
        seen.add(value);
        out.push(value);
      }
    }
    return out;
  };

  const readFirst = (keys: string[]): string | undefined => readFromStores(keys)[0];

  const parseExpiry = (raw: string | undefined): number | undefined => {
    if (!raw || !/^\d+$/.test(raw)) return undefined;
    const parsed = Number(raw);
    return parsed < 1_000_000_000_000 ? parsed * 1000 : parsed;
  };

  const profileWithBearer = async (accessToken: string): Promise<boolean> => {
    for (const path of profilePaths) {
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
  };

  const accessCandidates = readFromStores(accessTokenKeys);
  if (accessCandidates.length === 0) {
    return { kind: "not_logged_in", message: "没有找到 Sub2API access token，请先完成站点登录" };
  }

  for (const accessToken of accessCandidates) {
    if (await profileWithBearer(accessToken)) {
      return {
        kind: "candidate",
        candidate: {
          access_token: accessToken,
          refresh_token: readFirst(refreshTokenKeys),
          token_expires_at: parseExpiry(readFirst(expiresKeys)),
        },
      };
    }
  }

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
