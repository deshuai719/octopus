import type { ExtractResult } from "../types";

export async function extractAnyRouterUser(): Promise<ExtractResult> {
  const readNumber = (value: unknown): number | undefined => {
    const parsed = typeof value === "number" ? value : typeof value === "string" ? Number(value) : NaN;
    return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined;
  };
  for (const path of ["/api/user/self", "/api/user/profile"]) {
    try {
      const response = await fetch(new URL(path, location.origin), { credentials: "include", headers: { Accept: "application/json" } });
      if (response.status === 401) return { kind: "not_logged_in", message: "站点登录尚未完成" };
      if (!response.ok) continue;
      const payload = (await response.json()) as Record<string, unknown>;
      const data = (payload.data ?? payload) as Record<string, unknown>;
      const userID = readNumber(data.id ?? data.user_id);
      if (userID) {
        return {
          kind: "candidate",
          candidate: {
            platform_user_id: userID,
            identity_label: typeof data.username === "string" ? data.username : undefined,
          },
        };
      }
    } catch {
      // Try the next read-only profile endpoint once.
    }
  }
  const stored = localStorage.getItem("user_id") ?? sessionStorage.getItem("user_id");
  const userID = readNumber(stored);
  if (userID) return { kind: "candidate", candidate: { platform_user_id: userID } };
  return { kind: "not_logged_in", message: "未能读取 AnyRouter 用户 ID，请确认登录已完成" };
}
