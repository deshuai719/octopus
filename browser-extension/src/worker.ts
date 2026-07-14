import { extractAnyRouterUser } from "./adapters/anyrouter";
import { extractNewAPICredentials } from "./adapters/newapi";
import { extractSub2APICredentials } from "./adapters/sub2api";
import {
  bindingExpired,
  canonicalHTTPOrigin,
  getOctopusBinding,
  octopusAPI,
  readOctopusPageAuth,
  storeOctopusBinding,
  validateOctopusBinding,
} from "./binding";
import { recordDiagnostic, sanitizeDiagnosticMessage, clearDiagnostics } from "./diagnostics";
import {
  claimDirectTokenGeneration,
  clearAllDirectSessions,
  getDirectSession,
  putDirectSession,
  removeDirectSession,
} from "./direct-session";
import { recoveryCandidateFailureMessage } from "./errors";
import { httpOriginFromURL, parseRecoveryPacket, permissionPattern } from "./packet";
import { detectCurrentPlatform, PLATFORM_STATUS_SIGNATURES } from "./platform-detect";
import {
  claimNewAPITokenGeneration,
  discardRecoverySession,
  getRecoverySession,
  RECOVERY_ERROR_KEY,
  RECOVERY_EXPIRY_ALARM,
  storeRecoverySession,
} from "./session";
import type {
  CandidateCredential,
  DirectCaptureCandidate,
  DirectCaptureView,
  ExtractResult,
  OctopusBinding,
  Platform,
  RecoveryPacket,
  SessionEvent,
  WorkerRequest,
  WorkerResponse,
} from "./types";

let pendingReplacement: OctopusBinding | undefined;
const PENDING_REPLACEMENT_ORIGIN_KEY = "octopusPendingReplacementOriginV1";
const PENDING_REPLACEMENT_EXPIRY_ALARM = "octopus-pending-replacement-expiry";
const DIRECT_PERMISSION_ALARM_PREFIX = "octopus-direct-permission:";

function directPermissionAlarmName(origin: string): string {
  return DIRECT_PERMISSION_ALARM_PREFIX + encodeURIComponent(origin);
}

async function scheduleDirectPermissionExpiry(origin: string): Promise<void> {
  await chrome.alarms.create(directPermissionAlarmName(origin), { delayInMinutes: 10 });
}

async function revokeDirectPermission(origin: string): Promise<void> {
  await chrome.alarms.clear(directPermissionAlarmName(origin));
  await chrome.permissions.remove({ origins: [permissionPattern(origin)] });
}

async function revokePermissionUnlessBound(origin: string): Promise<void> {
  const binding = await getOctopusBinding();
  if (binding?.origin !== origin) await revokeDirectPermission(origin);
}

type RecoveryPacketPageResult =
  | { kind: "missing" }
  | { kind: "invalid_json" }
  | { kind: "packet"; value: unknown };

async function readPacketFromPage(tabId: number): Promise<RecoveryPacket | undefined> {
  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId },
    func: () => {
      const node = document.querySelector<HTMLScriptElement>('script[data-octopus-recovery="true"]');
      if (!node?.textContent) return { kind: "missing" } as const;
      const bridgeWindow = window as Window & { __octopusRecoveryBridge?: boolean };
      if (!bridgeWindow.__octopusRecoveryBridge) {
        bridgeWindow.__octopusRecoveryBridge = true;
        window.addEventListener("message", (event) => {
          if (
            event.source === window &&
            event.origin === location.origin &&
            event.data?.source === "octopus" &&
            event.data?.type === "octopus_auth_recovery_terminal" &&
            typeof event.data?.session_id === "string"
          ) {
            void chrome.runtime.sendMessage({
              type: "page_terminal",
              session_id: event.data.session_id,
            });
          }
        });
      }
      try {
        return { kind: "packet", value: JSON.parse(node.textContent) as unknown } as const;
      } catch {
        return { kind: "invalid_json" } as const;
      }
    },
  });
  const pageResult = result as RecoveryPacketPageResult | RecoveryPacket | undefined;
  if (!pageResult || typeof pageResult !== "object") throw new Error("恢复会话读取结果无效");
  if ("kind" in pageResult) {
    if (pageResult.kind === "missing") return undefined;
    if (pageResult.kind === "invalid_json") throw new Error("恢复会话 JSON 无效");
    return parseRecoveryPacket(pageResult.value);
  }
  // Accept the unwrapped result used by already-open extension pages during a
  // service-worker update; the next page read always uses the tagged shape.
  return parseRecoveryPacket(pageResult);
}

async function notifyPanel(event: SessionEvent): Promise<void> {
  try {
    await chrome.runtime.sendMessage(event);
  } catch {
    // The panel may still be loading; it also reads the persisted session on startup.
  }
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "无法读取恢复会话";
}

async function readPageAuth(tabId: number) {
  const [{ result }] = await chrome.scripting.executeScript({ target: { tabId }, func: readOctopusPageAuth });
  return result;
}

async function initializeOctopusBinding(origin: string, pageAuth: NonNullable<Awaited<ReturnType<typeof readPageAuth>>>): Promise<WorkerResponse> {
  if (!origin.startsWith("https://")) throw new Error("正式扩展只允许绑定 HTTPS Octopus");
  if (pageAuth.is_api_key_auth) throw new Error("当前是 Octopus API Key 登录，必须使用管理员 JWT 登录后初始化");
  if (Date.parse(pageAuth.expire_at) <= Date.now()) throw new Error("当前 Octopus 管理员 JWT 已过期，请重新登录");
  await validateOctopusBinding(origin, pageAuth.token);
  const next: OctopusBinding = {
    version: 1,
    origin,
    token: pageAuth.token,
    expire_at: pageAuth.expire_at,
    validated_at: new Date().toISOString(),
  };
  const current = await getOctopusBinding();
  if (current && current.origin !== origin) {
    pendingReplacement = next;
    await chrome.storage.session.set({ [PENDING_REPLACEMENT_ORIGIN_KEY]: origin });
    await chrome.alarms.create(PENDING_REPLACEMENT_EXPIRY_ALARM, { delayInMinutes: 10 });
    await notifyPanel({ type: "binding_replacement_required", current_origin: current.origin, next_origin: origin });
    return { ok: true, mode: "binding", message: `需要确认从 ${current.origin} 换绑到 ${origin}` };
  }
  await storeOctopusBinding(next);
  await notifyPanel({ type: "binding_updated", origin });
  return { ok: true, mode: "binding", origin, binding: { version: next.version, origin: next.origin, expire_at: next.expire_at, validated_at: next.validated_at } };
}

async function extractDirectCandidate(tabId: number, origin: string, platform: Platform, evidence: DirectCaptureCandidate["evidence"], allowTokenGeneration = false): Promise<ExtractResult> {
  if (["new-api", "one-api", "one-hub", "done-hub"].includes(platform)) {
    const [{ result }] = await chrome.scripting.executeScript({
      target: { tabId },
      func: extractNewAPICredentials,
      args: [{ allow_token_generation: allowTokenGeneration }],
    });
    return result ?? { kind: "error", message: "NewAPI 家族提取脚本没有返回结果" };
  }
  if (platform === "sub2api") {
    const [{ result }] = await chrome.scripting.executeScript({ target: { tabId }, func: extractSub2APICredentials });
    return result ?? { kind: "error", message: "Sub2API 提取脚本没有返回结果" };
  }
  const cookie = await chrome.cookies.get({ url: origin, name: "session" });
  if (!cookie?.value) return { kind: "not_logged_in", message: "目标域名没有 session Cookie，请先完成登录" };
  const [{ result }] = await chrome.scripting.executeScript({ target: { tabId }, func: extractAnyRouterUser });
  if (!result || result.kind !== "candidate" || !result.candidate?.platform_user_id) {
    return result ?? { kind: "error", message: "AnyRouter 用户信息提取失败" };
  }
  void evidence;
  return { kind: "candidate", candidate: { access_token: cookie.value, platform_user_id: result.candidate.platform_user_id, identity_label: result.candidate.identity_label } };
}

async function submitDirectPreview(binding: OctopusBinding, operationID: string, origin: string, platform: Platform, evidence: DirectCaptureCandidate["evidence"], extracted: ExtractResult): Promise<WorkerResponse> {
  if (extracted.kind !== "candidate") return { ok: extracted.kind !== "error", result: extracted };
  const candidate: DirectCaptureCandidate = { origin, platform, evidence, ...extracted.candidate };
  const capture = await octopusAPI<DirectCaptureView>(binding, "/api/v1/site/direct-capture/preview", operationID, { method: "POST", body: JSON.stringify(candidate) });
  await putDirectSession({ origin, operation_id: operationID, platform, capture_id: capture.capture_id, phase: capture.phase, expires_at: capture.expires_at, generation_attempted: extracted.generated_system_token === true, capture });
  await revokeDirectPermission(origin);
  await notifyPanel({ type: "direct_capture_updated", origin, capture });
  return { ok: true, mode: "direct", origin, capture };
}

async function startDirectCapture(tabId: number, origin: string): Promise<WorkerResponse> {
  const binding = await getOctopusBinding();
  if (!binding) throw new Error("尚未初始化 Octopus，请先在已登录 Octopus 页面点击扩展");
  if (bindingExpired(binding)) throw new Error("Octopus 管理员 JWT 已过期，请回到 Octopus 页面重新初始化");
  await validateOctopusBinding(binding.origin, binding.token);
  await scheduleDirectPermissionExpiry(origin);
  const operationID = crypto.randomUUID();
  const [{ result: discovery }] = await chrome.scripting.executeScript({
    target: { tabId },
    func: detectCurrentPlatform,
    args: [PLATFORM_STATUS_SIGNATURES],
  });
  if (!discovery?.platform) {
    const reason = discovery?.reason === "variant_inconclusive"
      ? "只能识别兼容家族或品牌，无法通过结构化证据确认具体平台；本次没有读取或上传凭据"
      : "无法可靠识别当前平台或登录状态；本次没有读取或上传凭据";
    throw new Error(reason);
  }
  const extracted = await extractDirectCandidate(tabId, origin, discovery.platform, discovery.evidence, false);
  if (extracted.kind === "manual_required" && extracted.reason === "system_token_missing") {
    await putDirectSession({
      origin,
      operation_id: operationID,
      platform: discovery.platform,
      evidence: discovery.evidence,
      platform_user_id: extracted.platform_user_id,
      identity_label: extracted.identity_label,
      phase: "credential_generation",
      expires_at: new Date(Date.now() + 10 * 60_000).toISOString(),
      generation_attempted: false,
    });
    await notifyPanel({ type: "direct_capture_manual_required", origin, operation_id: operationID, platform: discovery.platform, reason: extracted.reason, message: extracted.message });
    return { ok: true, mode: "direct", origin, result: extracted };
  }
  if (extracted.kind !== "candidate") {
    await revokeDirectPermission(origin);
    return { ok: false, mode: "direct", origin, result: extracted, message: extracted.message };
  }
  return submitDirectPreview(binding, operationID, origin, discovery.platform, discovery.evidence, extracted);
}

async function routeClickedTab(tab: chrome.tabs.Tab, permissionPromise: Promise<boolean>): Promise<WorkerResponse> {
  const origin = canonicalHTTPOrigin(tab.url);
  const operationID = crypto.randomUUID();
  try {
    if (!tab.id) throw new Error("无法读取当前标签页");
    const granted = await permissionPromise;
    if (!granted) throw new Error("未获得当前站点权限");
    const packet = await readPacketFromPage(tab.id);
    if (packet) {
      await storeRecoverySession(packet);
      await chrome.storage.session.remove(RECOVERY_ERROR_KEY);
      await notifyPanel({ type: "session_updated", packet });
      if (origin) await revokePermissionUnlessBound(origin);
      return { ok: true, mode: "legacy", origin, packet };
    }
    if (!origin) throw new Error("当前标签页不是可访问的 HTTP(S) 页面");
    const pageAuth = await readPageAuth(tab.id);
    if (pageAuth) {
      const response = await initializeOctopusBinding(origin, pageAuth);
      await chrome.storage.session.remove(RECOVERY_ERROR_KEY);
      return response;
    }
    const response = await startDirectCapture(tab.id, origin);
    if (!response.ok) throw new Error(response.message ?? "直接捕获失败");
    await chrome.storage.session.remove(RECOVERY_ERROR_KEY);
    return response;
  } catch (error) {
    const message = sanitizeDiagnosticMessage(error);
    await chrome.storage.session.set({ [RECOVERY_ERROR_KEY]: message });
    await recordDiagnostic({ at: new Date().toISOString(), operation_id: operationID, origin, error_code: (error as { error_code?: string })?.error_code ?? "extension.capture.failed", stage: (error as { stage?: string })?.stage ?? "credential_extraction", suggested_action: (error as { suggested_action?: string })?.suggested_action, http_status: (error as { http_status?: number })?.http_status, message });
    await notifyPanel({ type: "session_error", message });
    if (origin) await revokePermissionUnlessBound(origin);
    return { ok: false, origin, message };
  }
}

chrome.action.onClicked.addListener((tab) => {
  if (!tab.id) return;
  void chrome.sidePanel.open({ windowId: tab.windowId }).catch(async (error: unknown) => {
    await chrome.storage.session.set({ [RECOVERY_ERROR_KEY]: errorMessage(error) });
  });
  const origin = canonicalHTTPOrigin(tab.url);
  const permissionPromise = origin && chrome.permissions.request
    ? chrome.permissions.request({ origins: [permissionPattern(origin)] })
    : Promise.resolve(true);
  void routeClickedTab(tab, permissionPromise);
});

chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === RECOVERY_EXPIRY_ALARM) void discardRecoverySession();
  if (alarm.name === PENDING_REPLACEMENT_EXPIRY_ALARM) {
    void (async () => {
      const stored = await chrome.storage.session.get(PENDING_REPLACEMENT_ORIGIN_KEY);
      const origin = stored[PENDING_REPLACEMENT_ORIGIN_KEY];
      if (typeof origin === "string") await chrome.permissions.remove({ origins: [permissionPattern(origin)] });
      await chrome.storage.session.remove(PENDING_REPLACEMENT_ORIGIN_KEY);
      if (pendingReplacement?.origin === origin) pendingReplacement = undefined;
    })();
  }
  if (alarm.name.startsWith(DIRECT_PERMISSION_ALARM_PREFIX)) {
    void (async () => {
      const encodedOrigin = alarm.name.slice(DIRECT_PERMISSION_ALARM_PREFIX.length);
      let origin: string;
      try {
        origin = decodeURIComponent(encodedOrigin);
      } catch {
        return;
      }
      await removeDirectSession(origin);
      await chrome.permissions.remove({ origins: [permissionPattern(origin)] });
    })();
  }
});

async function findTargetTab(packet: RecoveryPacket) {
  const tabs = await chrome.tabs.query({ url: permissionPattern(packet.origin) });
  const tab = tabs.find((item) => item.active) ?? tabs.at(-1);
  if (!tab?.id) throw new Error("请先打开目标站点并完成登录");
  return tab.id;
}

async function extractCandidate(packet: RecoveryPacket): Promise<ExtractResult> {
  const tabId = await findTargetTab(packet);
  switch (packet.auth.compatible_family) {
    case "new-api": {
      const runExtraction = async (allowTokenGeneration: boolean): Promise<ExtractResult> => {
        const [{ result }] = await chrome.scripting.executeScript({
          target: { tabId },
          func: extractNewAPICredentials,
          args: [{ allow_token_generation: allowTokenGeneration }],
        });
        return result ?? { kind: "error", message: "NewAPI 提取脚本没有返回结果" };
      };
      let extracted = await runExtraction(false);
      if (extracted.kind === "manual_required" && extracted.reason === "system_token_missing") {
        if (!await claimNewAPITokenGeneration(packet.session_id)) {
          return {
            kind: "error",
            message: "本恢复会话已经尝试过自动生成系统令牌。为避免重复重置，请新建恢复会话或改用手动填写。",
          };
        }
        extracted = await runExtraction(true);
      }
      if (extracted.kind === "manual_required" && extracted.suggested_paths?.[0]) {
        const tokenPage = new URL(extracted.suggested_paths[0], packet.origin);
        if (tokenPage.origin === packet.origin) await chrome.tabs.update(tabId, { url: tokenPage.href, active: true });
      }
      return extracted;
    }
    case "sub2api": {
      const [{ result }] = await chrome.scripting.executeScript({ target: { tabId }, func: extractSub2APICredentials });
      return result ?? { kind: "error", message: "Sub2API 提取脚本没有返回结果" };
    }
    case "anyrouter": {
      const cookie = await chrome.cookies.get({ url: packet.origin, name: "session" });
      if (!cookie?.value) return { kind: "not_logged_in", message: "目标域名没有 session Cookie，请先完成登录" };
      const [{ result }] = await chrome.scripting.executeScript({ target: { tabId }, func: extractAnyRouterUser });
      if (!result || result.kind !== "candidate" || !result.candidate?.platform_user_id) {
        return result ?? { kind: "error", message: "AnyRouter 用户信息提取失败" };
      }
      return {
        kind: "candidate",
        candidate: {
          access_token: cookie.value,
          platform_user_id: result.candidate.platform_user_id,
          identity_label: result.candidate.identity_label,
        },
      };
    }
  }
}

async function submitCandidate(packet: RecoveryPacket, extracted: ExtractResult) {
  if (extracted.kind !== "candidate") return extracted;
  const candidate: CandidateCredential = {
    account_id: packet.account_id,
    origin: packet.origin,
    platform: packet.platform,
    ...extracted.candidate,
  };
  let response: Response;
  try {
    response = await fetch(
      `${packet.api_base_url}/api/v1/site/auth-recovery/${encodeURIComponent(packet.session_id)}/candidate`,
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-Octopus-Recovery-Capability": packet.capability,
        },
        body: JSON.stringify(candidate),
      },
    );
  } catch {
    await discardRecoverySession();
    return {
      kind: "error",
      message: extracted.generated_system_token
        ? "NewAPI 系统令牌已更新，但尚未保存到 Octopus。候选提交请求失败，请新建恢复会话或手动填写。"
        : "候选凭据提交请求失败，请新建恢复会话或手动填写。",
    } satisfies ExtractResult;
  }
  const payload = (await response.json().catch(() => null)) as { error_code?: string } | null;
  if (!response.ok) {
    await discardRecoverySession();
    return {
      kind: "error",
      message: extracted.generated_system_token
        ? `NewAPI 系统令牌已更新，但尚未保存到 Octopus。${recoveryCandidateFailureMessage(payload?.error_code, response.status)}。本次 capability 已结束，请新建恢复会话或改用手动填写。`
        : `${recoveryCandidateFailureMessage(payload?.error_code, response.status)}。本次 capability 已结束，请回到 Octopus 新建恢复会话或改用手动填写。`,
    } satisfies ExtractResult;
  }
  await discardRecoverySession();
  return extracted;
}

chrome.runtime.onMessage.addListener((request: WorkerRequest, sender, sendResponse: (response: WorkerResponse) => void) => {
  void (async () => {
    try {
      if (request.type === "get_session") {
        const packet = await getRecoverySession();
        const stored = packet ? {} : await chrome.storage.session.get(RECOVERY_ERROR_KEY);
        const message = typeof stored[RECOVERY_ERROR_KEY] === "string" ? stored[RECOVERY_ERROR_KEY] : undefined;
        sendResponse({ ok: true, packet, message });
        return;
      }
      if (request.type === "capture_active_session") {
        let tab: chrome.tabs.Tab;
        try {
          const [activeTab] = await chrome.tabs.query({ active: true, currentWindow: true });
          if (!activeTab?.id) throw new Error("无法读取当前标签页");
          const activeOrigin = httpOriginFromURL(activeTab.url);
          if (activeOrigin !== request.expected_origin) {
            throw new Error("当前标签页已切换，请重新点击读取");
          }
          const hasPermission = await chrome.permissions.contains({ origins: [permissionPattern(activeOrigin)] });
          if (!hasPermission) throw new Error("当前页面权限不存在，请重新点击读取");
          tab = activeTab;
        } catch (error) {
          await revokePermissionUnlessBound(request.expected_origin);
          throw error;
        }
        sendResponse(await routeClickedTab(tab, Promise.resolve(true)));
        return;
      }
      if (request.type === "discard_session") {
        await discardRecoverySession();
        sendResponse({ ok: true });
        return;
      }
      if (request.type === "page_terminal") {
        const packet = await getRecoverySession();
        const senderOrigin = sender.tab?.url ? new URL(sender.tab.url).origin : "";
        if (packet && request.session_id === packet.session_id && senderOrigin === packet.api_base_url) {
          await discardRecoverySession();
        }
        sendResponse({ ok: true });
        return;
      }
      if (request.type === "get_active_context") {
        const origin = request.origin;
        const packet = await getRecoverySession();
        if (packet && (!origin || packet.origin === origin || packet.api_base_url === origin)) {
          sendResponse({ ok: true, mode: "legacy", origin, packet });
          return;
        }
        if (origin) {
          const indexed = await getDirectSession(origin);
          if (indexed?.capture_id) {
            const binding = await getOctopusBinding();
            if (!binding) throw new Error("Octopus 绑定不存在");
            const capture = await octopusAPI<DirectCaptureView>(binding, `/api/v1/site/direct-capture/${encodeURIComponent(indexed.capture_id)}`, indexed.operation_id);
            await putDirectSession({ ...indexed, phase: capture.phase, expires_at: capture.expires_at, capture });
            sendResponse({ ok: true, mode: "direct", origin, capture });
            return;
          }
          if (indexed) {
            sendResponse({ ok: true, mode: "direct", origin, message: indexed.phase });
            return;
          }
        }
        const binding = await getOctopusBinding();
        if (binding && origin === binding.origin) {
          sendResponse({ ok: true, mode: "binding", origin, binding: { version: binding.version, origin: binding.origin, expire_at: binding.expire_at, validated_at: binding.validated_at } });
          return;
        }
        sendResponse({ ok: true, mode: "unsupported", origin, message: binding ? "请在已登录的受支持中转站页面点击扩展" : "请先在已登录 Octopus 页面点击扩展完成初始化" });
        return;
      }
      if (request.type === "confirm_binding_replacement") {
        if (!pendingReplacement) throw new Error("没有等待确认的 Octopus 换绑");
        const current = await getOctopusBinding();
        const next = pendingReplacement;
        await validateOctopusBinding(next.origin, next.token);
        await storeOctopusBinding(next);
        pendingReplacement = undefined;
        await chrome.alarms.clear(PENDING_REPLACEMENT_EXPIRY_ALARM);
        await chrome.storage.session.remove(PENDING_REPLACEMENT_ORIGIN_KEY);
        for (const item of await clearAllDirectSessions()) {
          await revokeDirectPermission(item.origin);
        }
        await discardRecoverySession();
        if (current && current.origin !== next.origin) await chrome.permissions.remove({ origins: [permissionPattern(current.origin)] });
        await notifyPanel({ type: "binding_updated", origin: next.origin });
        sendResponse({ ok: true, mode: "binding", origin: next.origin });
        return;
      }
      if (request.type === "generate_direct_token") {
        const indexed = await getDirectSession(request.origin);
        if (!indexed?.platform || !(await claimDirectTokenGeneration(request.origin, indexed.operation_id))) {
          throw new Error("本次捕获已经尝试生成系统令牌；为避免重复覆盖，不会再次生成");
        }
        const tabs = await chrome.tabs.query({ url: permissionPattern(request.origin) });
        const tab = tabs.find((item) => item.active) ?? tabs.at(-1);
        if (!tab?.id) throw new Error("请保持目标站点标签页打开");
        const evidence = indexed.evidence ?? [];
        const extracted = await extractDirectCandidate(tab.id, request.origin, indexed.platform, evidence, true);
        const binding = await getOctopusBinding();
        if (!binding) throw new Error("Octopus 绑定不存在");
        const response = await submitDirectPreview(binding, indexed.operation_id, request.origin, indexed.platform, evidence, extracted);
        sendResponse(response);
        return;
      }
      if (request.type === "submit_direct_token") {
        const indexed = await getDirectSession(request.origin);
        const token = request.access_token.trim();
        if (!indexed?.platform || !indexed.evidence?.length) throw new Error("直接捕获会话不存在，请重新点击扩展识别站点");
        if (token.length === 0 || token.length > 16 * 1024) throw new Error("推荐令牌为空或超过 16 KiB 限制");
        const binding = await getOctopusBinding();
        if (!binding) throw new Error("Octopus 绑定不存在");
        const response = await submitDirectPreview(binding, indexed.operation_id, request.origin, indexed.platform, indexed.evidence, {
          kind: "candidate",
          candidate: {
            access_token: token,
            platform_user_id: indexed.platform_user_id,
            identity_label: indexed.identity_label,
          },
        });
        sendResponse(response);
        return;
      }
      if (request.type === "confirm_direct_capture" || request.type === "resolve_direct_capture" || request.type === "cancel_direct_capture" || request.type === "retry_direct_sync") {
        const indexed = await getDirectSession(request.origin);
        const binding = await getOctopusBinding();
        if (!indexed || !binding) throw new Error("直接捕获会话或 Octopus 绑定不存在");
        let path: string;
        let body: Record<string, unknown>;
        if (request.type === "confirm_direct_capture") {
          path = `/api/v1/site/direct-capture/${encodeURIComponent(request.capture_id)}/confirm`;
          body = { preview_version: request.preview_version, site_name: request.site_name, account_name: request.account_name };
        } else if (request.type === "resolve_direct_capture") {
          path = `/api/v1/site/direct-capture/${encodeURIComponent(request.capture_id)}/resolve`;
          body = { account_id: request.account_id, create_new: request.create_new === true };
        } else if (request.type === "cancel_direct_capture") {
          path = `/api/v1/site/direct-capture/${encodeURIComponent(request.capture_id)}/cancel`;
          body = {};
        } else {
          path = `/api/v1/site/direct-capture/${encodeURIComponent(request.capture_id)}/retry-sync`;
          body = {};
        }
        const capture = await octopusAPI<DirectCaptureView>(binding, path, indexed.operation_id, { method: "POST", body: JSON.stringify(body) });
        await putDirectSession({ ...indexed, capture_id: capture.capture_id, phase: capture.phase, expires_at: capture.expires_at, capture });
        if (["completed", "sync_failed", "canceled", "failed", "conflict", "expired"].includes(capture.phase)) {
          await revokeDirectPermission(request.origin);
        }
        await notifyPanel({ type: "direct_capture_updated", origin: request.origin, capture });
        sendResponse({ ok: true, mode: "direct", origin: request.origin, capture });
        return;
      }
      if (request.type === "clear_diagnostics") {
        await clearDiagnostics();
        sendResponse({ ok: true });
        return;
      }
      const packet = await getRecoverySession();
      if (!packet) throw new Error("没有活动恢复会话，请回到 Octopus 页面并点击扩展图标");
      if (request.type === "open_target") {
        const hasPermission = await chrome.permissions.contains({ origins: [permissionPattern(packet.origin)] });
        if (!hasPermission) throw new Error("请先授权目标站点临时权限");
        await chrome.tabs.create({ url: packet.origin, active: true });
        sendResponse({ ok: true, packet });
        return;
      }
      if (request.type === "extract_and_submit") {
        const hasPermission = await chrome.permissions.contains({ origins: [permissionPattern(packet.origin)] });
        if (!hasPermission) throw new Error("请先授权并打开目标站点");
        const result = await submitCandidate(packet, await extractCandidate(packet));
        sendResponse({ ok: result.kind !== "error", result });
      }
    } catch (error) {
      const message = sanitizeDiagnosticMessage(error);
      const origin = "origin" in request && typeof request.origin === "string" ? request.origin : undefined;
      try {
        const indexed = origin ? await getDirectSession(origin) : undefined;
        await recordDiagnostic({
          at: new Date().toISOString(),
          operation_id: indexed?.operation_id ?? crypto.randomUUID(),
          origin,
          platform: indexed?.platform,
          error_code: (error as { error_code?: string })?.error_code ?? "extension.operation.failed",
          stage: (error as { stage?: string })?.stage ?? "confirmation",
          retryable: (error as { retryable?: boolean })?.retryable,
          suggested_action: (error as { suggested_action?: string })?.suggested_action,
          http_status: (error as { http_status?: number })?.http_status,
          message,
        });
      } catch {
        // Diagnostic persistence must never hide the original operation error.
      }
      sendResponse({ ok: false, message });
    }
  })();
  return true;
});
