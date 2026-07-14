import type { ExtractResult } from "../types";

export async function extractSub2APICredentials(): Promise<ExtractResult> {
  const stores = [localStorage, sessionStorage];
  const read = (keys: string[]) => {
    for (const store of stores) {
      for (const key of keys) {
        const value = store.getItem(key)?.trim();
        if (value) return value.replace(/^Bearer\s+/i, "");
      }
    }
    return undefined;
  };
  const accessToken = read(["auth_token", "access_token", "sub2api_access_token"]);
  const refreshToken = read(["refresh_token", "sub2api_refresh_token"]);
  const expiresRaw = read(["expires_at", "token_expires_at"]);
  if (!accessToken) {
    return { kind: "not_logged_in", message: "没有找到 Sub2API access token，请先完成站点登录" };
  }
  let tokenExpiresAt: number | undefined;
  if (expiresRaw && /^\d+$/.test(expiresRaw)) {
    const parsed = Number(expiresRaw);
    tokenExpiresAt = parsed < 1_000_000_000_000 ? parsed * 1000 : parsed;
  }
  return {
    kind: "candidate",
    candidate: {
      access_token: accessToken,
      refresh_token: refreshToken,
      token_expires_at: tokenExpiresAt,
    },
  };
}
