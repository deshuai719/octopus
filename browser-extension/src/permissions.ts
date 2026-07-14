import { permissionPattern } from "./packet";
import type { WorkerResponse } from "./types";

export async function runWithTargetPermission(
  origin: string,
  action: () => Promise<WorkerResponse>,
): Promise<WorkerResponse> {
  const granted = await chrome.permissions.request({ origins: [permissionPattern(origin)] });
  if (!granted) return { ok: false, message: "未获得目标站点临时权限" };
  return action();
}

export function grantPermissionAndOpenTarget(
  origin: string,
  openTarget: () => Promise<WorkerResponse>,
): Promise<WorkerResponse> {
  return runWithTargetPermission(origin, openTarget);
}

export async function runWithTemporaryPagePermission(
  origin: string,
  action: () => Promise<WorkerResponse>,
): Promise<WorkerResponse> {
  // Start the request before the first await so Chromium still sees the side-panel click as a user gesture.
  const permission = { origins: [permissionPattern(origin)] };
  const permissionRequest = chrome.permissions.request(permission);
  const granted = await permissionRequest;
  if (!granted) return { ok: false, message: "未获得当前页面临时权限" };
  try {
    return await action();
  } finally {
    const removed = await chrome.permissions.remove(permission);
    if (!removed) throw new Error("无法撤销当前页面临时权限，请在扩展设置中手动移除");
  }
}
