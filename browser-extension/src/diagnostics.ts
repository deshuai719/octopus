const DIAGNOSTICS_KEY = "octopusDiagnosticErrorsV1";
const MAX_ERRORS = 20;
const MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000;

export type DiagnosticError = {
  at: string;
  operation_id: string;
  origin?: string;
  platform?: string;
  error_code: string;
  stage: string;
  retryable?: boolean;
  http_status?: number;
  suggested_action?: string;
  message: string;
};

const forbiddenKey = /authorization|cookie|token|password|jwt|secret|credential|request_body/i;

export function sanitizeDiagnosticMessage(value: unknown): string {
  const text = value instanceof Error ? value.message : String(value ?? "操作失败");
  return text
    .replace(/Bearer\s+[A-Za-z0-9._~+\/-]+/gi, "Bearer [REDACTED]")
    .replace(/(token|cookie|password|jwt|secret)\s*[=:]\s*[^\s,;]+/gi, "$1=[REDACTED]")
    .slice(0, 512);
}

export async function recordDiagnostic(error: DiagnosticError): Promise<void> {
  if (!chrome.storage.local) return;
  const stored = await chrome.storage.local.get(DIAGNOSTICS_KEY);
  const existing = Array.isArray(stored[DIAGNOSTICS_KEY]) ? stored[DIAGNOSTICS_KEY] as DiagnosticError[] : [];
  const cutoff = Date.now() - MAX_AGE_MS;
  const safe = Object.fromEntries(Object.entries(error).filter(([key]) => !forbiddenKey.test(key))) as unknown as DiagnosticError;
  safe.message = sanitizeDiagnosticMessage(error.message);
  const next = [...existing.filter((item) => Date.parse(item.at) >= cutoff), safe].slice(-MAX_ERRORS);
  await chrome.storage.local.set({ [DIAGNOSTICS_KEY]: next });
}

export async function listDiagnostics(): Promise<DiagnosticError[]> {
  if (!chrome.storage.local) return [];
  const stored = await chrome.storage.local.get(DIAGNOSTICS_KEY);
  const existing = Array.isArray(stored[DIAGNOSTICS_KEY]) ? stored[DIAGNOSTICS_KEY] as DiagnosticError[] : [];
  const cutoff = Date.now() - MAX_AGE_MS;
  return existing
    .filter((item) => item && typeof item === "object" && Date.parse(item.at) >= cutoff)
    .slice(-MAX_ERRORS)
    .map((item) => ({ ...item, message: sanitizeDiagnosticMessage(item.message) }));
}

export function formatDiagnostics(items: DiagnosticError[]): string {
  return JSON.stringify(items.map((item) => ({
    at: item.at,
    operation_id: item.operation_id,
    origin: item.origin,
    platform: item.platform,
    error_code: item.error_code,
    stage: item.stage,
    retryable: item.retryable,
    http_status: item.http_status,
    suggested_action: item.suggested_action,
    message: sanitizeDiagnosticMessage(item.message),
  })), null, 2);
}

export async function clearDiagnostics(): Promise<void> {
  if (!chrome.storage.local) return;
  await chrome.storage.local.remove(DIAGNOSTICS_KEY);
}
