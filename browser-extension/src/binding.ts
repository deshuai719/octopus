import type { OctopusBinding } from "./types";

export const OCTOPUS_BINDING_KEY = "octopusBindingV1";
const DIRECT_CAPTURE_VERSION_HEADER = "X-Octopus-Direct-Capture-Version";

export type OctopusPageAuth = {
  token: string;
  expire_at: string;
  is_api_key_auth: boolean;
};

export function canonicalHTTPOrigin(raw: string | undefined): string | undefined {
  if (!raw) return undefined;
  try {
    const url = new URL(raw);
    if (url.protocol !== "https:" && url.protocol !== "http:") return undefined;
    return url.origin.toLowerCase();
  } catch {
    return undefined;
  }
}

export function permissionPattern(origin: string): string {
  return `${new URL(origin).origin}/*`;
}

export async function getOctopusBinding(): Promise<OctopusBinding | undefined> {
  const stored = await chrome.storage.local.get(OCTOPUS_BINDING_KEY);
  const value = stored[OCTOPUS_BINDING_KEY];
  if (!value || typeof value !== "object") return undefined;
  const item = value as Partial<OctopusBinding>;
  if (
    item.version !== 1 ||
    typeof item.origin !== "string" ||
    canonicalHTTPOrigin(item.origin) !== item.origin ||
    !item.origin.startsWith("https://") ||
    typeof item.token !== "string" ||
    !item.token ||
    typeof item.expire_at !== "string" ||
    Number.isNaN(Date.parse(item.expire_at)) ||
    typeof item.validated_at !== "string"
  ) return undefined;
  return item as OctopusBinding;
}

export async function storeOctopusBinding(binding: OctopusBinding): Promise<void> {
  if (!binding.origin.startsWith("https://") || canonicalHTTPOrigin(binding.origin) !== binding.origin) {
    throw new Error("正式扩展只允许绑定规范化的 HTTPS Octopus origin");
  }
  await chrome.storage.local.set({ [OCTOPUS_BINDING_KEY]: binding });
}

export async function clearOctopusBinding(): Promise<void> {
  await chrome.storage.local.remove(OCTOPUS_BINDING_KEY);
}

export function bindingExpired(binding: OctopusBinding): boolean {
  return Date.parse(binding.expire_at) <= Date.now();
}

export async function validateOctopusBinding(origin: string, token: string): Promise<void> {
  if (!origin.startsWith("https://") || canonicalHTTPOrigin(origin) !== origin) {
    throw new Error("正式扩展只允许绑定规范化的 HTTPS Octopus origin");
  }
  const response = await fetch(`${origin}/api/v1/user/status`, {
    method: "GET",
    headers: { Authorization: `Bearer ${token}`, "X-Octopus-Operation-ID": crypto.randomUUID() },
    redirect: "error",
  });
  if (response.status === 401) throw new Error("Octopus 管理员 JWT 已失效，请重新登录");
  if (!response.ok) throw new Error(`Octopus 管理员状态验证失败（HTTP ${response.status}）`);
  if (response.headers.get(DIRECT_CAPTURE_VERSION_HEADER) !== "1") {
    throw new Error("当前 Octopus 版本不支持直接捕获，请先升级 Octopus");
  }
  if (new URL(response.url).origin !== origin) throw new Error("Octopus 验证响应发生了跨站跳转");
}

export async function octopusAPI<T>(binding: OctopusBinding, path: string, operationID: string, init: RequestInit = {}): Promise<T> {
  if (bindingExpired(binding)) throw new Error("Octopus 管理员 JWT 已过期，请回到 Octopus 页面重新初始化");
  if (!path.startsWith("/api/v1/site/direct-capture/")) throw new Error("扩展拒绝访问未授权的 Octopus API 路径");
  const headers = new Headers(init.headers);
  headers.set("Content-Type", "application/json");
  headers.set("Authorization", `Bearer ${binding.token}`);
  headers.set("X-Octopus-Operation-ID", operationID);
  const response = await fetch(binding.origin + path, {
    ...init,
    redirect: "error",
    headers,
  });
  if (new URL(response.url).origin !== binding.origin) throw new Error("Octopus API 响应发生了跨站跳转");
  const payload = await response.json().catch(() => null) as {
    data?: T;
    error_code?: string;
    message?: string;
    stage?: string;
    retryable?: boolean;
    suggested_action?: string;
  } | null;
  if (!response.ok) {
    const error = new Error(payload?.message || `Octopus 请求失败（HTTP ${response.status}）`);
    Object.assign(error, { error_code: payload?.error_code, stage: payload?.stage, retryable: payload?.retryable, suggested_action: payload?.suggested_action, http_status: response.status });
    throw error;
  }
  if (!payload || !("data" in payload)) throw new Error("Octopus 返回了无效响应");
  return payload.data as T;
}

export function readOctopusPageAuth(): OctopusPageAuth | undefined {
  try {
    const raw = localStorage.getItem("auth-storage");
    if (!raw) return undefined;
    const parsed = JSON.parse(raw) as unknown;
    if (!parsed || typeof parsed !== "object") return undefined;
    const root = parsed as Record<string, unknown>;
    const state = root.state && typeof root.state === "object" ? root.state as Record<string, unknown> : root;
    const token = typeof state.token === "string" ? state.token.trim() : "";
    const expireAt = typeof state.expireAt === "string" ? state.expireAt : "";
    if (!token || !expireAt || Number.isNaN(Date.parse(expireAt))) return undefined;
    return { token, expire_at: expireAt, is_api_key_auth: state.isAPIKeyAuth === true };
  } catch {
    return undefined;
  }
}
