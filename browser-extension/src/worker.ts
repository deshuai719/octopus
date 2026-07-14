import { extractAnyRouterUser } from "./adapters/anyrouter";
import { extractNewAPICredentials } from "./adapters/newapi";
import { extractSub2APICredentials } from "./adapters/sub2api";
import { recoveryCandidateFailureMessage } from "./errors";
import { httpOriginFromURL, parseRecoveryPacket, permissionPattern } from "./packet";
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
  ExtractResult,
  RecoveryPacket,
  SessionEvent,
  WorkerRequest,
  WorkerResponse,
} from "./types";

async function readPacketFromPage(tabId: number): Promise<RecoveryPacket> {
  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId },
    func: () => {
      const node = document.querySelector<HTMLScriptElement>('script[data-octopus-recovery="true"]');
      if (!node?.textContent) throw new Error("当前页面没有可读取的 Octopus 恢复会话");
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
      return JSON.parse(node.textContent) as unknown;
    },
  });
  return parseRecoveryPacket(result);
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

async function captureRecoverySession(tabId: number): Promise<WorkerResponse> {
  try {
    const packet = await readPacketFromPage(tabId);
    await storeRecoverySession(packet);
    await chrome.storage.session.remove(RECOVERY_ERROR_KEY);
    await notifyPanel({ type: "session_updated", packet });
    return { ok: true, packet };
  } catch (error) {
    const message = errorMessage(error);
    await chrome.storage.session.set({ [RECOVERY_ERROR_KEY]: message });
    await notifyPanel({ type: "session_error", message });
    return { ok: false, message };
  }
}

chrome.action.onClicked.addListener((tab) => {
  if (!tab.id) return;
  void chrome.sidePanel.open({ windowId: tab.windowId }).catch(async (error: unknown) => {
    await chrome.storage.session.set({ [RECOVERY_ERROR_KEY]: errorMessage(error) });
  });
  void captureRecoverySession(tab.id);
});

chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === RECOVERY_EXPIRY_ALARM) void discardRecoverySession();
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
        const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
        if (!tab?.id) throw new Error("无法读取当前标签页");
        if (httpOriginFromURL(tab.url) !== request.expected_origin) {
          throw new Error("当前标签页已切换，请重新点击读取");
        }
        sendResponse(await captureRecoverySession(tab.id));
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
      sendResponse({ ok: false, message: error instanceof Error ? error.message : "扩展操作失败" });
    }
  })();
  return true;
});
