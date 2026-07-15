import { permissionPattern } from "./binding";
import type { WorkerResponse } from "./types";

export async function runWithTargetPermission(
  origin: string,
  action: () => Promise<WorkerResponse>,
): Promise<WorkerResponse> {
  const granted = await chrome.permissions.request({ origins: [permissionPattern(origin)] });
  if (!granted) return { ok: false, message: "未获得目标站点临时权限" };
  return action();
}

export async function runWithRoutedPagePermission(
  origin: string,
  action: () => Promise<WorkerResponse>,
): Promise<WorkerResponse> {
  const permission = { origins: [permissionPattern(origin)] };
  const granted = await chrome.permissions.request(permission);
  if (!granted) return { ok: false, message: "未获得当前页面临时权限" };
  try {
    // The worker owns the successful lifecycle: Octopus binding retains the
    // origin, while direct capture revokes temporary site permissions when done.
    return await action();
  } catch (error) {
    const removed = await chrome.permissions.remove(permission);
    if (!removed) {
      throw new Error("扩展通信失败，且无法撤销当前页面权限；请在扩展设置中手动移除", { cause: error });
    }
    throw error;
  }
}
