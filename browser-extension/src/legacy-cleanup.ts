import { getOctopusBinding, permissionPattern } from "./binding";

const LEGACY_RECOVERY_SESSION_KEY = "octopusRecoverySession";
const LEGACY_RECOVERY_ERROR_KEY = "octopusRecoveryError";
const LEGACY_GENERATION_KEY = "octopusNewAPITokenGenerationSession";
const LEGACY_EXPIRY_ALARM = "octopusRecoveryExpiry";

type LegacyRecoverySession = { origin?: unknown };

export async function clearLegacyRecoveryState(): Promise<void> {
  const stored = await chrome.storage.session.get(LEGACY_RECOVERY_SESSION_KEY);
  const legacy = stored[LEGACY_RECOVERY_SESSION_KEY] as LegacyRecoverySession | undefined;
  await chrome.storage.session.remove([
    LEGACY_RECOVERY_SESSION_KEY,
    LEGACY_RECOVERY_ERROR_KEY,
    LEGACY_GENERATION_KEY,
  ]);
  await chrome.alarms.clear(LEGACY_EXPIRY_ALARM);
  const binding = await getOctopusBinding();
  if (legacy && typeof legacy.origin === "string" && legacy.origin !== binding?.origin) {
    await chrome.permissions.remove({ origins: [permissionPattern(legacy.origin)] });
  }
}
