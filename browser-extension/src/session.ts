import { parseRecoveryPacket, permissionPattern } from "./packet";
import type { RecoveryPacket } from "./types";

export const RECOVERY_SESSION_KEY = "octopusRecoverySession";
export const RECOVERY_ERROR_KEY = "octopusRecoveryError";
export const RECOVERY_EXPIRY_ALARM = "octopusRecoveryExpiry";
export const NEWAPI_TOKEN_GENERATION_SESSION_KEY = "octopusNewAPITokenGenerationSession";

const newAPITokenGenerationClaims = new Set<string>();

export async function claimNewAPITokenGeneration(sessionID: string): Promise<boolean> {
  if (newAPITokenGenerationClaims.has(sessionID)) return false;
  newAPITokenGenerationClaims.add(sessionID);
  try {
    const stored = await chrome.storage.session.get(NEWAPI_TOKEN_GENERATION_SESSION_KEY);
    if (stored[NEWAPI_TOKEN_GENERATION_SESSION_KEY] === sessionID) return false;
    await chrome.storage.session.set({ [NEWAPI_TOKEN_GENERATION_SESSION_KEY]: sessionID });
    return true;
  } catch (error) {
    newAPITokenGenerationClaims.delete(sessionID);
    throw error;
  }
}

export async function storeRecoverySession(packet: RecoveryPacket): Promise<void> {
  await chrome.storage.session.set({ [RECOVERY_SESSION_KEY]: packet });
  await chrome.alarms.create(RECOVERY_EXPIRY_ALARM, { when: Date.parse(packet.expires_at) });
}

export async function getRecoverySession(): Promise<RecoveryPacket | undefined> {
  const stored = await chrome.storage.session.get(RECOVERY_SESSION_KEY);
  if (!stored[RECOVERY_SESSION_KEY]) return undefined;
  try {
    return parseRecoveryPacket(stored[RECOVERY_SESSION_KEY]);
  } catch {
    await discardRecoverySession();
    return undefined;
  }
}

export async function discardRecoverySession(): Promise<void> {
  const stored = await chrome.storage.session.get(RECOVERY_SESSION_KEY);
  const value = stored[RECOVERY_SESSION_KEY];
  if (value && typeof value === "object" && "session_id" in value && typeof value.session_id === "string") {
    newAPITokenGenerationClaims.delete(value.session_id);
  }
  await chrome.storage.session.remove([
    RECOVERY_SESSION_KEY,
    RECOVERY_ERROR_KEY,
    NEWAPI_TOKEN_GENERATION_SESSION_KEY,
  ]);
  await chrome.alarms.clear(RECOVERY_EXPIRY_ALARM);
  if (value && typeof value === "object" && "origin" in value && typeof value.origin === "string") {
    await chrome.permissions.remove({ origins: [permissionPattern(value.origin)] });
  }
}
