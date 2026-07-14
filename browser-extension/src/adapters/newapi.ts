import type { ExtractResult } from "../types";

type NewAPIExtractionOptions = {
  allow_token_generation?: boolean;
};

export async function extractNewAPICredentials(
  options: NewAPIExtractionOptions = {},
): Promise<ExtractResult> {
  const readNumber = (value: unknown): number | undefined => {
    if (typeof value === "number" && Number.isInteger(value) && value > 0) return value;
    if (typeof value === "string" && /^\d+$/.test(value)) {
      const parsed = Number(value);
      if (Number.isSafeInteger(parsed) && parsed > 0) return parsed;
    }
    return undefined;
  };
  const readString = (value: unknown): string | undefined =>
    typeof value === "string" && value.trim() ? value.trim() : undefined;
  const readCompleteToken = (value: unknown): string | undefined => {
    const candidate = readString(value);
    return candidate && !candidate.includes("****") && candidate.length >= 16
      ? candidate
      : undefined;
  };
  const nested = (value: unknown, ...keys: string[]): unknown => {
    let current = value;
    for (const key of keys) {
      if (typeof current !== "object" || current === null) return undefined;
      current = (current as Record<string, unknown>)[key];
    }
    return current;
  };
  let generatedSystemToken = false;

  let browserUser: unknown;
  try {
    const storedUser = localStorage.getItem("user");
    if (storedUser) browserUser = JSON.parse(storedUser) as unknown;
  } catch {
    // A malformed or stale frontend cache is not proof of an authenticated session.
  }
  const browserUserID = readNumber(nested(browserUser, "id"));
  const profileHeaders: Record<string, string> = { Accept: "application/json" };
  if (browserUserID) profileHeaders["New-API-User"] = String(browserUserID);

  let profile: unknown;
  let profileVerified = false;
  let sawUnauthorized = false;
  for (const path of ["/api/user/self", "/api/user/profile"]) {
    try {
      const response = await fetch(new URL(path, location.origin), {
        credentials: "include",
        headers: profileHeaders,
      });
      if (response.status === 401) {
        sawUnauthorized = true;
        continue;
      }
      if (response.ok) {
        profile = await response.json();
        profileVerified = true;
        break;
      }
    } catch {
      // Try the next read-only profile endpoint once.
    }
  }
  if (!profileVerified) {
    return sawUnauthorized
      ? { kind: "not_logged_in", message: "站点登录尚未完成" }
      : { kind: "not_logged_in", message: "未能通过站点接口确认当前登录用户" };
  }
  const userID =
    readNumber(nested(profile, "data", "id")) ??
    readNumber(nested(profile, "data", "user", "id")) ??
    readNumber(nested(profile, "id")) ??
    browserUserID;
  if (!userID) return { kind: "not_logged_in", message: "未能确认当前登录用户，请先在站点完成登录" };

  const selectors = [
    'input[name="system_access_token"]',
    'input[id*="system-access-token"]',
    'textarea[name="system_access_token"]',
    '[data-system-access-token]',
  ];
  const tokenValue = (element: Element | null): string | undefined => {
    if (!element) return undefined;
    const value =
      element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement
        ? element.value
        : element.getAttribute("data-system-access-token") ?? element.textContent;
    return readCompleteToken(value);
  };
  let accessToken: string | undefined;
  for (const selector of selectors) {
    accessToken = tokenValue(document.querySelector(selector));
    if (accessToken) {
      break;
    }
  }
  if (!accessToken) {
    const tokenLabels = new Set(["系统访问令牌", "系統存取權杖", "System Access Token"]);
    const labels = document.querySelectorAll<HTMLElement>("h1,h2,h3,h4,h5,h6,label");
    for (const label of Array.from(labels)) {
      if (!tokenLabels.has(label.textContent?.trim() ?? "")) continue;
      let container = label.parentElement;
      for (let depth = 0; container && depth < 4; depth += 1, container = container.parentElement) {
        accessToken = tokenValue(container.querySelector("input[readonly], textarea[readonly]"));
        if (accessToken) break;
      }
      if (accessToken) break;
    }
  }
  if (!accessToken) {
    for (const storage of [localStorage, sessionStorage]) {
      for (const key of ["system_access_token", "new_api_system_access_token"]) {
        const candidate = readCompleteToken(storage.getItem(key));
        if (candidate) {
          accessToken = candidate;
          break;
        }
      }
      if (accessToken) break;
    }
  }
  if (!accessToken) {
    if (options.allow_token_generation) {
      try {
        const response = await fetch(new URL("/api/user/token", location.origin), {
          credentials: "include",
          headers: profileHeaders,
        });
        if (response.status === 401) {
          return { kind: "not_logged_in", message: "生成系统访问令牌时登录状态已失效" };
        }
        if (!response.ok) {
          return { kind: "error", message: `系统访问令牌生成失败（HTTP ${response.status}）` };
        }
        const payload = await response.json() as unknown;
        const generated = nested(payload, "success") === true
          ? readCompleteToken(nested(payload, "data"))
          : undefined;
        if (!generated) {
          return { kind: "error", message: "系统访问令牌生成失败或响应格式无效" };
        }
        accessToken = generated;
        generatedSystemToken = true;
      } catch {
        return { kind: "error", message: "系统访问令牌生成请求失败" };
      }
    } else {
      return {
        kind: "manual_required",
        reason: "system_token_missing",
        message: "已确认登录，但页面没有完整系统访问令牌",
        suggested_paths: ["/console/personal", "/setting", "/profile"],
      };
    }
  }
  if (!accessToken) {
    return {
      kind: "error",
      message: "未能读取或生成完整系统访问令牌",
    };
  }
  return {
    kind: "candidate",
    generated_system_token: generatedSystemToken || undefined,
    candidate: {
      access_token: accessToken,
      platform_user_id: userID,
      identity_label:
        readString(nested(profile, "data", "username")) ??
        readString(nested(profile, "data", "user", "username")) ??
        readString(nested(browserUser, "username")),
    },
  };
}
