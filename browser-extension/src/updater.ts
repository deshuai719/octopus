export const UPDATE_ALARM_NAME = "octopus-extension-update-check";
export const UPDATE_STATE_KEY = "octopusExtensionUpdateStateV1";
export const UPDATE_CHECK_TTL_MS = 6 * 60 * 60 * 1000;
export const UPDATE_ALARM_PERIOD_MINUTES = 24 * 60;
export const NATIVE_HOST_NAME = "com.octopus.extension_updater";
const MANUAL_INSTALLER_MESSAGE = "更新助手已下载。请从浏览器下载记录中手动运行 octopus-extension-helper.exe，完成后返回并点击“检测助手”。";

export type UpdatePhase =
  | "idle"
  | "checking"
  | "host_required"
  | "target_required"
  | "ready"
  | "update_available"
  | "installer_downloading"
  | "installer_ready"
  | "installing"
  | "updating"
  | "updated"
  | "rolling_back"
  | "rolled_back"
  | "error";

export type UpdateTarget = {
  browser: string;
  profile: string;
  path: string;
  status?: string;
};

export type ExtensionUpdateState = {
  version: 1;
  phase: UpdatePhase;
  current_version: string;
  latest_version?: string;
  update_available: boolean;
  updater_version?: string;
  targets: UpdateTarget[];
  message: string;
  checked_at?: string;
  installer_download_id?: number;
  error_code?: string;
  retryable?: boolean;
};

export type NativeUpdateResponse = {
  protocol_version: 1;
  request_id: string;
  kind: "progress" | "result";
  ok: boolean;
  stage?: string;
  updater_version?: string;
  current_version?: string;
  latest_version?: string;
  update_available?: boolean;
  targets?: UpdateTarget[];
  error?: { code: string; message: string; retryable: boolean };
};

export function manualInstallerPending(state: ExtensionUpdateState): boolean {
  return (state.phase === "installer_ready" || state.phase === "installing")
    && Number.isInteger(state.installer_download_id);
}

export function defaultUpdateState(): ExtensionUpdateState {
  return {
    version: 1,
    phase: "idle",
    current_version: chrome.runtime.getManifest().version,
    update_available: false,
    targets: [],
    message: "尚未检查扩展更新。",
  };
}

export async function loadUpdateState(): Promise<ExtensionUpdateState> {
  const stored = await chrome.storage.local.get(UPDATE_STATE_KEY);
  const candidate = stored[UPDATE_STATE_KEY] as Partial<ExtensionUpdateState> | undefined;
  const fallback = defaultUpdateState();
  if (!candidate || candidate.version !== 1 || typeof candidate.phase !== "string") return fallback;
  return {
    ...fallback,
    ...candidate,
    current_version: fallback.current_version,
    targets: Array.isArray(candidate.targets) ? candidate.targets : [],
  };
}

export async function patchUpdateState(patch: Partial<ExtensionUpdateState>): Promise<ExtensionUpdateState> {
  const current = await loadUpdateState();
  const next: ExtensionUpdateState = {
    ...current,
    ...patch,
    version: 1,
    current_version: chrome.runtime.getManifest().version,
    targets: patch.targets ?? current.targets,
  };
  await chrome.storage.local.set({ [UPDATE_STATE_KEY]: next });
  return next;
}

export function compareExtensionVersions(left: string, right: string): number {
  const parse = (value: string): number[] => {
    const parts = value.split(".");
    if (parts.length === 0 || parts.length > 4 || parts.some((part) => !/^\d+$/.test(part))) {
      throw new Error(`扩展版本格式无效：${value}`);
    }
    return [...parts.map(Number), 0, 0, 0, 0].slice(0, 4);
  };
  const a = parse(left);
  const b = parse(right);
  for (let index = 0; index < 4; index += 1) {
    if (a[index] < b[index]) return -1;
    if (a[index] > b[index]) return 1;
  }
  return 0;
}

export function checkIsFresh(state: ExtensionUpdateState, now = Date.now()): boolean {
  if (!state.checked_at) return false;
  const checkedAt = Date.parse(state.checked_at);
  return Number.isFinite(checkedAt) && now - checkedAt < UPDATE_CHECK_TTL_MS;
}

function isNativeUpdateResponse(value: unknown): value is NativeUpdateResponse {
  if (!value || typeof value !== "object") return false;
  const candidate = value as Partial<NativeUpdateResponse>;
  return candidate.protocol_version === 1
    && typeof candidate.request_id === "string"
    && (candidate.kind === "progress" || candidate.kind === "result")
    && typeof candidate.ok === "boolean";
}

export function sendNativeUpdateAction(
  action: "status" | "check" | "update" | "rollback" | "select_target",
  onProgress?: (message: NativeUpdateResponse) => void,
): Promise<NativeUpdateResponse> {
  return new Promise((resolve, reject) => {
    const requestID = crypto.randomUUID();
    let settled = false;
    let port: chrome.runtime.Port;
    try {
      port = chrome.runtime.connectNative(NATIVE_HOST_NAME);
    } catch {
      reject(new Error("本机更新助手尚未启用"));
      return;
    }
    port.onMessage.addListener((message: unknown) => {
      if (!isNativeUpdateResponse(message) || message.request_id !== requestID) return;
      if (message.kind === "progress") {
        onProgress?.(message);
        return;
      }
      settled = true;
      port.disconnect();
      if (!message.ok) {
        const error = new Error(message.error?.message ?? "本机更新助手执行失败") as Error & { code?: string; retryable?: boolean };
        error.code = message.error?.code;
        error.retryable = message.error?.retryable;
        reject(error);
        return;
      }
      resolve(message);
    });
    port.onDisconnect.addListener(() => {
      if (settled) return;
      settled = true;
      const detail = chrome.runtime.lastError?.message;
      reject(new Error(detail ? `本机更新助手不可用：${detail}` : "本机更新助手连接已断开"));
    });
    port.postMessage({
      protocol_version: 1,
      request_id: requestID,
      action,
      current_version: chrome.runtime.getManifest().version,
    });
  });
}

function stateFromNative(message: NativeUpdateResponse, fallbackPhase: UpdatePhase, fallbackMessage: string): Partial<ExtensionUpdateState> {
  const updateAvailable = message.update_available === true
    || (!!message.latest_version && compareExtensionVersions(chrome.runtime.getManifest().version, message.latest_version) < 0);
  return {
    phase: updateAvailable ? "update_available" : fallbackPhase,
    latest_version: message.latest_version,
    update_available: updateAvailable,
    updater_version: message.updater_version,
    targets: message.targets ?? [],
    message: updateAvailable ? `发现新版本 ${message.latest_version}` : fallbackMessage,
    error_code: undefined,
    retryable: undefined,
  };
}

export async function refreshUpdaterStatus(): Promise<ExtensionUpdateState> {
  const current = await loadUpdateState();
  try {
    const response = await sendNativeUpdateAction("status");
    return patchUpdateState({
      ...stateFromNative(response, response.stage === "target_required" ? "target_required" : "ready", response.stage === "target_required" ? "更新助手已启用，但尚未识别扩展目录。" : "自动更新支持已启用。"),
    });
  } catch {
    if (manualInstallerPending(current)) {
      return patchUpdateState({
        phase: "installer_ready",
        message: MANUAL_INSTALLER_MESSAGE,
        error_code: undefined,
        retryable: undefined,
      });
    }
    return patchUpdateState({
      phase: "host_required",
      update_available: false,
      targets: [],
      message: "首次更新需要先启用本机更新助手。",
      error_code: "updater.host.unavailable",
      retryable: true,
    });
  }
}

export async function checkForExtensionUpdate(force = false): Promise<ExtensionUpdateState> {
  const current = await loadUpdateState();
  if (!force && checkIsFresh(current)) return current;
  await patchUpdateState({ phase: "checking", message: "正在检查 GitHub Releases…", error_code: undefined });
  try {
    const response = await sendNativeUpdateAction("check");
    return patchUpdateState({
      ...stateFromNative(response, "ready", "当前已是最新版。"),
      checked_at: new Date().toISOString(),
    });
  } catch (error) {
    const typed = error as Error & { code?: string; retryable?: boolean };
    const hostUnavailable = !typed.code;
    if (hostUnavailable && manualInstallerPending(current)) {
      return patchUpdateState({
        phase: "installer_ready",
        message: MANUAL_INSTALLER_MESSAGE,
        error_code: undefined,
        retryable: undefined,
      });
    }
    return patchUpdateState({
      phase: hostUnavailable ? "host_required" : "error",
      message: hostUnavailable ? "首次更新需要先启用本机更新助手。" : typed.message,
      error_code: typed.code ?? "updater.host.unavailable",
      retryable: typed.retryable ?? true,
    });
  }
}

export async function startExtensionUpdate(): Promise<{ state: ExtensionUpdateState; reload: boolean }> {
  await patchUpdateState({ phase: "updating", message: "正在准备扩展更新…", error_code: undefined });
  try {
    const runUpdate = () => sendNativeUpdateAction("update", (progress) => {
      void patchUpdateState({
        phase: "updating",
        updater_version: progress.updater_version,
        latest_version: progress.latest_version,
        targets: progress.targets ?? [],
        message: updateStageMessage(progress.stage),
      });
    });
    let response = await runUpdate();
    if (response.stage === "updater_restarting") {
      await patchUpdateState({ phase: "installing", message: "更新助手已升级，正在重新连接并继续本次更新…" });
      await new Promise((resolve) => setTimeout(resolve, 1_000));
      response = await runUpdate();
    }
    if (response.stage === "up_to_date") {
      return {
        state: await patchUpdateState({
          phase: "ready",
          latest_version: response.latest_version,
          update_available: false,
          updater_version: response.updater_version,
          targets: response.targets ?? [],
          checked_at: new Date().toISOString(),
          message: "当前已是最新版。",
        }),
        reload: false,
      };
    }
    const state = await patchUpdateState({
      phase: "updated",
      latest_version: response.latest_version,
      update_available: false,
      updater_version: response.updater_version,
      targets: response.targets ?? [],
      checked_at: new Date().toISOString(),
      message: `扩展已更新到 ${response.latest_version ?? "新版本"}，正在重新加载。`,
    });
    return { state, reload: response.stage === "completed" };
  } catch (error) {
    const typed = error as Error & { code?: string; retryable?: boolean };
    return {
      state: await patchUpdateState({
        phase: "error",
        message: typed.message,
        error_code: typed.code ?? "updater.update.failed",
        retryable: typed.retryable ?? true,
      }),
      reload: false,
    };
  }
}

export async function rollbackExtensionUpdate(): Promise<{ state: ExtensionUpdateState; reload: boolean }> {
  await patchUpdateState({ phase: "rolling_back", message: "正在恢复上一版本…", error_code: undefined });
  try {
    const response = await sendNativeUpdateAction("rollback", (progress) => {
      void patchUpdateState({ phase: "rolling_back", targets: progress.targets ?? [], message: updateStageMessage(progress.stage) });
    });
    return {
      state: await patchUpdateState({ phase: "rolled_back", targets: response.targets ?? [], message: "上一版本已恢复，正在重新加载扩展。" }),
      reload: response.stage === "rolled_back",
    };
  } catch (error) {
    const typed = error as Error & { code?: string; retryable?: boolean };
    return {
      state: await patchUpdateState({ phase: "error", message: typed.message, error_code: typed.code ?? "updater.rollback.failed", retryable: typed.retryable ?? false }),
      reload: false,
    };
  }
}

export async function selectExtensionTarget(): Promise<ExtensionUpdateState> {
  try {
    const response = await sendNativeUpdateAction("select_target");
    return patchUpdateState({
      phase: response.stage === "selection_canceled" ? "target_required" : "ready",
      targets: response.targets ?? [],
      message: response.stage === "selection_canceled" ? "没有选择扩展目录。" : "扩展目录已确认。",
    });
  } catch (error) {
    const typed = error as Error & { code?: string; retryable?: boolean };
    return patchUpdateState({ phase: "error", message: typed.message, error_code: typed.code ?? "updater.target.failed", retryable: typed.retryable ?? true });
  }
}

export async function prepareBundledUpdaterDownload(): Promise<ExtensionUpdateState> {
  await patchUpdateState({ phase: "installer_downloading", message: "正在准备内置更新助手…", error_code: undefined });
  let objectURL: string | undefined;
  try {
    const [binaryResponse, integrityResponse] = await Promise.all([
      fetch(chrome.runtime.getURL("octopus-extension-updater.exe")),
      fetch(chrome.runtime.getURL("updater-integrity.json")),
    ]);
    if (!binaryResponse.ok || !integrityResponse.ok) throw new Error("内置更新助手文件不完整，请重新下载扩展 ZIP");
    const binary = await binaryResponse.arrayBuffer();
    const integrity = await integrityResponse.json() as { sha256?: string; size?: number };
    const digest = await sha256Hex(binary);
    if (integrity.sha256 !== digest || integrity.size !== binary.byteLength) {
      throw new Error("内置更新助手校验失败，请重新下载扩展 ZIP");
    }
    objectURL = URL.createObjectURL(new Blob([binary], { type: "application/vnd.microsoft.portable-executable" }));
    const downloadID = await chrome.downloads.download({
      url: objectURL,
      filename: "Octopus/octopus-extension-helper.exe",
      conflictAction: "overwrite",
      saveAs: false,
    });
    await waitForDownload(downloadID);
    return patchUpdateState({
      phase: "installer_ready",
      installer_download_id: downloadID,
      message: MANUAL_INSTALLER_MESSAGE,
    });
  } catch (error) {
    return patchUpdateState({
      phase: "error",
      message: error instanceof Error ? error.message : "无法准备更新助手",
      error_code: "updater.installer.prepare_failed",
      retryable: true,
    });
  } finally {
    if (objectURL) URL.revokeObjectURL(objectURL);
  }
}


export async function sha256Hex(payload: ArrayBuffer): Promise<string> {
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", payload));
  return Array.from(digest, (value) => value.toString(16).padStart(2, "0")).join("");
}

function waitForDownload(downloadID: number): Promise<void> {
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      chrome.downloads.onChanged.removeListener(listener);
      reject(new Error("更新助手下载未完成，请检查浏览器下载提示"));
    }, 120_000);
    const listener = (delta: chrome.downloads.DownloadDelta) => {
      if (delta.id !== downloadID) return;
      if (delta.error?.current) {
        clearTimeout(timeout);
        chrome.downloads.onChanged.removeListener(listener);
        reject(new Error("更新助手下载失败，请检查浏览器下载提示"));
      } else if (delta.state?.current === "complete") {
        clearTimeout(timeout);
        chrome.downloads.onChanged.removeListener(listener);
        resolve();
      }
    };
    chrome.downloads.onChanged.addListener(listener);
    void chrome.downloads.search({ id: downloadID }).then(([item]) => {
      if (item?.state === "complete") {
        clearTimeout(timeout);
        chrome.downloads.onChanged.removeListener(listener);
        resolve();
      }
    }).catch(() => undefined);
  });
}

function updateStageMessage(stage?: string): string {
  return ({
    downloading: "正在从 GitHub 下载扩展包…",
    verified: "签名和 SHA-256 校验通过，正在准备替换…",
    replacing: "正在备份并替换扩展目录…",
    completed: "扩展文件替换完成。",
    rolling_back: "正在恢复上一版本…",
  } as Record<string, string>)[stage ?? ""] ?? "正在处理扩展更新…";
}
