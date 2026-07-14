import {
  grantPermissionAndOpenTarget,
  runWithTargetPermission,
  runWithTemporaryPagePermission,
} from "./permissions";
import { httpOriginFromURL } from "./packet";
import {
  candidateDeliveredView,
  loginRequiredView,
  readingView,
  readyView,
  requestingPermissionView,
  retryExtractionView,
  submittingView,
  waitingView,
} from "./panel-state";
import type {
  PanelPrimaryAction,
  PanelView,
} from "./panel-state";
import type {
  RecoveryPacket,
  SessionEvent,
  WorkerRequest,
  WorkerResponse,
} from "./types";

const status = document.querySelector<HTMLElement>("#status")!;
const detail = document.querySelector<HTMLElement>("#detail")!;
const steps = document.querySelector<HTMLOListElement>("#steps")!;
const endpoints = document.querySelector<HTMLElement>("#endpoints")!;
const apiOrigin = document.querySelector<HTMLElement>("#api-origin")!;
const targetOrigin = document.querySelector<HTMLElement>("#target-origin")!;
const primaryButton = document.querySelector<HTMLButtonElement>("#primary-action")!;
const discardButton = document.querySelector<HTMLButtonElement>("#discard")!;

let currentPacket: RecoveryPacket | undefined;
let primaryAction: PanelPrimaryAction = "capture";
let capturePageOrigin: string | undefined;

function send(request: WorkerRequest) {
  return chrome.runtime.sendMessage<WorkerRequest, WorkerResponse>(request);
}

function renderView(view: PanelView): void {
  status.textContent = view.status;
  detail.textContent = view.detail;
  primaryAction = view.primaryAction;
  primaryButton.textContent = view.primaryLabel;
  primaryButton.disabled = view.primaryDisabled;
}

function renderPacket(packet: RecoveryPacket): void {
  currentPacket = packet;
  endpoints.hidden = false;
  apiOrigin.textContent = packet.api_base_url;
  targetOrigin.textContent = packet.origin;
  steps.replaceChildren(
    ...packet.auth.recovery_guide.steps.map((text) => {
      const item = document.createElement("li");
      item.textContent = text;
      return item;
    }),
  );
  renderView(readyView(packet.auth.recovery_guide.title));
}

function clearPacketDisplay(): void {
  currentPacket = undefined;
  endpoints.hidden = true;
  apiOrigin.textContent = "—";
  targetOrigin.textContent = "—";
  steps.replaceChildren();
}

async function refreshCapturePageOrigin(): Promise<void> {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  capturePageOrigin = httpOriginFromURL(tab?.url);
}

async function captureActiveSession(): Promise<void> {
  const expectedOrigin = capturePageOrigin;
  if (!expectedOrigin) {
    renderView(waitingView("当前标签页不是可访问的 HTTP(S) 页面，请切回 Octopus 恢复页面后重试。"));
    void refreshCapturePageOrigin();
    return;
  }
  renderView(readingView());
  let response: WorkerResponse;
  try {
    response = await runWithTemporaryPagePermission(
      expectedOrigin,
      () => send({ type: "capture_active_session", expected_origin: expectedOrigin }),
    );
  } catch (error) {
    renderView(waitingView(error instanceof Error ? error.message : "无法读取当前标签页"));
    return;
  }
  if (response.packet) renderPacket(response.packet);
  else renderView(waitingView(response.message ?? "当前页面没有可读取的 Octopus 恢复会话。"));
}

async function grantAndOpen(): Promise<void> {
  const packet = currentPacket;
  if (!packet) {
    renderView(waitingView("没有活动恢复会话。请切到 Octopus 恢复页面后再点击工具栏图标。"));
    return;
  }
  renderView(requestingPermissionView());
  try {
    const response = await grantPermissionAndOpenTarget(packet.origin, () => send({ type: "open_target" }));
    if (!response.ok) renderView(readyView(response.message ?? "无法打开目标站点"));
    else renderView(loginRequiredView(packet.auth.compatible_family === "new-api"));
  } catch (error) {
    renderView(readyView(error instanceof Error ? error.message : "无法申请目标站点临时权限"));
  }
}

async function extractAndSubmit(): Promise<void> {
  const packet = currentPacket;
  if (!packet) {
    renderView(waitingView("没有活动恢复会话。请切到 Octopus 恢复页面后再点击工具栏图标。"));
    return;
  }
  const canGenerateSystemToken = packet.auth.compatible_family === "new-api";
  renderView(submittingView(canGenerateSystemToken));
  let response: WorkerResponse;
  try {
    response = await runWithTargetPermission(packet.origin, () => send({ type: "extract_and_submit" }));
  } catch (error) {
    renderView(retryExtractionView(
      "无法访问目标站点",
      error instanceof Error ? error.message : "无法重新确认目标站点临时权限",
      "重新授权并提取",
    ));
    return;
  }
  const result = response.result;
  if (!response.ok && !result) {
    renderView(retryExtractionView(
      "操作失败",
      response.message ?? "扩展操作失败",
      "重新授权并提取",
    ));
  } else if (result?.kind === "candidate") {
    currentPacket = undefined;
    renderView(candidateDeliveredView());
  } else if (result?.kind === "manual_required") {
    renderView(retryExtractionView(
      "需要手动创建令牌",
      `${result.message} 已打开站内令牌设置候选页。`,
      "令牌准备好后重新提取",
    ));
  } else if (result?.kind === "not_logged_in") {
    renderView(retryExtractionView(
      "尚未完成登录",
      result.message,
      canGenerateSystemToken ? "生成/读取系统令牌并验证" : "我已完成登录，重新提取",
    ));
  } else if (result) {
    renderView(retryExtractionView("提取失败", result.message));
  }
}

async function load(): Promise<void> {
  renderView(readingView());
  await refreshCapturePageOrigin();
  const response = await send({ type: "get_session" });
  if (response.packet) renderPacket(response.packet);
  else renderView(waitingView(response.message));
}

primaryButton.addEventListener("click", () => {
  if (primaryAction === "capture") void captureActiveSession();
  else if (primaryAction === "grant") void grantAndOpen();
  else if (primaryAction === "extract") void extractAndSubmit();
});

discardButton.addEventListener("click", async () => {
  await send({ type: "discard_session" });
  clearPacketDisplay();
  renderView(waitingView("会话和临时站点权限已清理。请在 Octopus 恢复页面重新点击扩展图标。"));
});

chrome.runtime.onMessage.addListener((event: SessionEvent) => {
  if (event.type === "session_updated") renderPacket(event.packet);
  else if (event.type === "session_error" && !currentPacket) renderView(waitingView(event.message));
});

chrome.tabs.onActivated.addListener(() => {
  void refreshCapturePageOrigin();
});

chrome.tabs.onUpdated.addListener((_tabId, changeInfo, tab) => {
  if (tab.active && changeInfo.url) capturePageOrigin = httpOriginFromURL(changeInfo.url);
});

void load();
