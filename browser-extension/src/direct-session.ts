import type { DirectCaptureSummaryItem, DirectCaptureView, Platform, PlatformEvidence } from "./types";

export const DIRECT_CAPTURE_INDEX_KEY = "octopusDirectCaptureIndexV1";
const ACTIVE_DIRECT_PHASES = new Set(["credential_generation", "resolution_required", "preview_ready", "confirming", "saved_syncing"]);

export type DirectSessionIndexItem = {
  origin: string;
  operation_id: string;
  platform?: Platform;
  evidence?: PlatformEvidence[];
  platform_user_id?: number;
  identity_label?: string;
  page_title?: string;
  capture_id?: string;
  phase: string;
  expires_at: string;
  generation_attempted: boolean;
  capture?: DirectCaptureView;
  created_at?: string;
  updated_at?: string;
  sync_started_at?: string;
};

async function readIndex(): Promise<Record<string, DirectSessionIndexItem>> {
  const stored = await chrome.storage.session.get(DIRECT_CAPTURE_INDEX_KEY);
  const value = stored[DIRECT_CAPTURE_INDEX_KEY];
  if (!value || typeof value !== "object") return {};
  const now = Date.now();
  return Object.fromEntries(Object.entries(value as Record<string, DirectSessionIndexItem>).filter(([, item]) =>
    item && typeof item.origin === "string" && typeof item.operation_id === "string" && Date.parse(item.expires_at) > now,
  ));
}

async function writeIndex(index: Record<string, DirectSessionIndexItem>): Promise<void> {
  await chrome.storage.session.set({ [DIRECT_CAPTURE_INDEX_KEY]: index });
}

export async function getDirectSession(origin: string): Promise<DirectSessionIndexItem | undefined> {
  return (await readIndex())[origin];
}

export async function putDirectSession(item: DirectSessionIndexItem): Promise<void> {
  const index = await readIndex();
  const previous = index[item.origin];
  const now = new Date().toISOString();
  index[item.origin] = {
    ...item,
    created_at: previous?.created_at ?? item.created_at ?? now,
    updated_at: now,
    sync_started_at: item.sync_started_at ?? previous?.sync_started_at ?? (item.phase === "saved_syncing" ? now : undefined),
  };
  await writeIndex(index);
}

export async function listDirectSessions(): Promise<DirectSessionIndexItem[]> {
  return Object.values(await readIndex());
}

export function directCaptureNeedsRefresh(phase: string): boolean {
  return phase === "saved_syncing";
}

export function directCaptureIsActive(phase: string): boolean {
  return ACTIVE_DIRECT_PHASES.has(phase);
}

export async function getDirectCaptureSummary(limit = 20): Promise<DirectCaptureSummaryItem[]> {
  const items = await listDirectSessions();
  return items
    .sort((a, b) => Number(directCaptureIsActive(b.phase)) - Number(directCaptureIsActive(a.phase)) || Date.parse(b.updated_at ?? b.created_at ?? "") - Date.parse(a.updated_at ?? a.created_at ?? ""))
    .slice(0, limit)
    .map((item) => ({
      origin: item.origin,
      capture_id: item.capture_id,
      site_name: item.capture?.site_name,
      phase: item.phase,
      saved: Boolean(item.capture?.saved) || ["saved_syncing", "sync_failed", "completed"].includes(item.phase),
      created_at: item.created_at ?? item.updated_at ?? new Date().toISOString(),
      updated_at: item.updated_at ?? item.created_at ?? new Date().toISOString(),
      sync_started_at: item.sync_started_at,
      expires_at: item.expires_at,
      can_retry_sync: item.phase === "sync_failed",
      can_clear: !directCaptureIsActive(item.phase),
      error_code: item.capture?.error_code,
      error_message: item.capture?.error_message,
      sync_result: item.capture?.sync_result,
    }));
}

export async function removeDirectSession(origin: string): Promise<void> {
  const index = await readIndex();
  delete index[origin];
  await writeIndex(index);
}

export function shouldRemoveDirectSession(phase: string): boolean {
  void phase;
  return false;
}

export async function claimDirectTokenGeneration(origin: string, operationID: string): Promise<boolean> {
  const index = await readIndex();
  const item = index[origin];
  if (!item || item.operation_id !== operationID || item.generation_attempted) return false;
  item.generation_attempted = true;
  await writeIndex(index);
  return true;
}

export async function clearAllDirectSessions(): Promise<DirectSessionIndexItem[]> {
  const items = Object.values(await readIndex());
  await chrome.storage.session.remove(DIRECT_CAPTURE_INDEX_KEY);
  return items;
}
