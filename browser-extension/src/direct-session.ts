import type { DirectCaptureView, Platform, PlatformEvidence } from "./types";

const DIRECT_CAPTURE_INDEX_KEY = "octopusDirectCaptureIndexV1";

export type DirectSessionIndexItem = {
  origin: string;
  operation_id: string;
  platform?: Platform;
  evidence?: PlatformEvidence[];
  platform_user_id?: number;
  identity_label?: string;
  capture_id?: string;
  phase: string;
  expires_at: string;
  generation_attempted: boolean;
  capture?: DirectCaptureView;
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
  index[item.origin] = item;
  await writeIndex(index);
}

export async function removeDirectSession(origin: string): Promise<void> {
  const index = await readIndex();
  delete index[origin];
  await writeIndex(index);
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
