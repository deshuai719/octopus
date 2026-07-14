import { grantPermissionAndOpenTarget, runWithRoutedPagePermission, runWithTargetPermission } from "./permissions";
import { clearDiagnostics, formatDiagnostics, listDiagnostics } from "./diagnostics";
import { httpOriginFromURL } from "./packet";
import { candidateDeliveredView, loginRequiredView, readingView, readyView, requestingPermissionView, retryExtractionView, submittingView, waitingView } from "./panel-state";
import type { PanelPrimaryAction, PanelView } from "./panel-state";
import type { DirectCaptureView, RecoveryPacket, SessionEvent, WorkerRequest, WorkerResponse } from "./types";

const status = document.querySelector<HTMLElement>("#status")!;
const detail = document.querySelector<HTMLElement>("#detail")!;
const steps = document.querySelector<HTMLOListElement>("#steps")!;
const endpoints = document.querySelector<HTMLElement>("#endpoints")!;
const apiOrigin = document.querySelector<HTMLElement>("#api-origin")!;
const targetOrigin = document.querySelector<HTMLElement>("#target-origin")!;
const primaryButton = document.querySelector<HTMLButtonElement>("#primary-action")!;
const secondaryButton = document.querySelector<HTMLButtonElement>("#secondary-action")!;
const discardButton = document.querySelector<HTMLButtonElement>("#discard")!;
const copyDiagnosticsButton = document.querySelector<HTMLButtonElement>("#copy-diagnostics")!;
const clearDiagnosticsButton = document.querySelector<HTMLButtonElement>("#clear-diagnostics")!;
const directPreview = document.querySelector<HTMLElement>("#direct-preview")!;
const captureAction = document.querySelector<HTMLElement>("#capture-action")!;
const credentialMask = document.querySelector<HTMLElement>("#credential-mask")!;
const platformUserID = document.querySelector<HTMLElement>("#platform-user-id")!;
const siteNameField = document.querySelector<HTMLElement>("#site-name-field")!;
const accountNameField = document.querySelector<HTMLElement>("#account-name-field")!;
const accountResolutionField = document.querySelector<HTMLElement>("#account-resolution-field")!;
const siteNameInput = document.querySelector<HTMLInputElement>("#site-name")!;
const accountNameInput = document.querySelector<HTMLInputElement>("#account-name")!;
const accountResolution = document.querySelector<HTMLSelectElement>("#account-resolution")!;
const captureWarning = document.querySelector<HTMLElement>("#capture-warning")!;
const manualTokenField = document.querySelector<HTMLElement>("#manual-token-field")!;
const manualTokenInput = document.querySelector<HTMLInputElement>("#manual-token")!;
const manualTokenButton = document.querySelector<HTMLButtonElement>("#manual-token-action")!;

let currentPacket: RecoveryPacket | undefined;
let currentCapture: DirectCaptureView | undefined;
let currentOrigin: string | undefined;
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
  secondaryButton.hidden = true;
}

function clearDirectPreview(): void {
  currentCapture = undefined;
  directPreview.hidden = true;
  siteNameField.hidden = true;
  accountNameField.hidden = true;
  accountResolutionField.hidden = true;
  captureWarning.hidden = true;
  accountResolution.replaceChildren();
  manualTokenInput.value = "";
  manualTokenField.hidden = true;
  manualTokenButton.hidden = true;
}

function showManualTokenInput(): void {
  manualTokenField.hidden = false;
  manualTokenButton.hidden = false;
}

function renderPacket(packet: RecoveryPacket): void {
  clearDirectPreview();
  currentPacket = packet;
  endpoints.hidden = false;
  apiOrigin.textContent = packet.api_base_url;
  targetOrigin.textContent = packet.origin;
  steps.replaceChildren(...packet.auth.recovery_guide.steps.map((text) => {
    const item = document.createElement("li");
    item.textContent = text;
    return item;
  }));
  renderView(readyView(packet.auth.recovery_guide.title));
}

function captureActionLabel(action: string | undefined): string {
  switch (action) {
    case "create_site_account": return "新建站点和账号";
    case "create_account": return "为已有站点新增账号";
    case "update_account": return "更新已有账号";
    default: return "等待确认";
  }
}

function renderCapture(capture: DirectCaptureView): void {
  currentPacket = undefined;
  currentCapture = capture;
  currentOrigin = capture.origin || currentOrigin;
  endpoints.hidden = false;
  targetOrigin.textContent = capture.origin;
  steps.replaceChildren();
  directPreview.hidden = false;
  captureAction.textContent = captureActionLabel(capture.action);
  credentialMask.textContent = capture.candidate?.access_token_mask ?? "已在确认后清除";
  platformUserID.textContent = capture.candidate?.platform_user_id?.toString() ?? "未提供";
  siteNameInput.value = capture.site_name ?? "";
  accountNameInput.value = capture.candidate?.identity_label || capture.account_name || "默认账号";
  siteNameField.hidden = capture.action !== "create_site_account";
  accountNameField.hidden = !["create_site_account", "create_account"].includes(capture.action ?? "");
  accountResolutionField.hidden = capture.phase !== "resolution_required";
  accountResolution.replaceChildren(...(capture.account_options ?? []).map((account) => {
    const option = document.createElement("option");
    option.value = String(account.id);
    option.textContent = `${account.name}${account.platform_user_id ? ` · ID ${account.platform_user_id}` : ""}${account.enabled ? "" : " · 已禁用"}`;
    return option;
  }));
  manualTokenInput.value = "";
  manualTokenField.hidden = true;
  manualTokenButton.hidden = true;
  const warnings: string[] = [];
  if (capture.site_archived) warnings.push("确认后将恢复归档站点，但不会自动启用路由。");
  if (capture.site_enabled === false || capture.account_enabled === false) warnings.push("站点或账号仍处于禁用状态，保存后不会自动加入路由。");
  if (capture.credential_migration) warnings.push("确认后凭据将从用户名密码切换为访问令牌，旧密码会被清除。");
  captureWarning.textContent = warnings.join(" ");
  captureWarning.hidden = warnings.length === 0;

  switch (capture.phase) {
    case "resolution_required":
      renderView({ status: "需要选择账号", detail: "当前站点有多个管理账号，请选择要更新的账号，或明确创建新账号。", primaryAction: "resolve_direct", primaryLabel: "使用选择的账号", primaryDisabled: false });
      secondaryButton.hidden = false;
      secondaryButton.textContent = "创建新账号";
      break;
    case "preview_ready":
      renderView({ status: "等待确认", detail: "请核对脱敏摘要。只有点击确认后才会写入 Octopus。", primaryAction: "confirm_direct", primaryLabel: "确认创建或更新", primaryDisabled: false });
      break;
    case "saved_syncing":
      renderView({ status: "账号已保存", detail: "凭据已安全保存，正在后台执行完整同步。", primaryAction: null, primaryLabel: "正在同步", primaryDisabled: true });
      break;
    case "sync_failed":
      renderView({ status: "账号已保存，同步失败", detail: capture.error_message ?? "可以主动重试同步，不会再次写入凭据。", primaryAction: "retry_sync", primaryLabel: "重试同步", primaryDisabled: false });
      break;
    case "completed":
      renderView({ status: "创建或更新完成", detail: capture.sync_result?.message ?? "账号保存和完整同步均已完成。", primaryAction: null, primaryLabel: "已完成", primaryDisabled: true });
      break;
    case "canceled":
      renderView(waitingView("本次直接捕获已取消，临时权限已清理。"));
      break;
    default:
      renderView({ status: "正在处理", detail: capture.error_message ?? `当前阶段：${capture.phase}`, primaryAction: null, primaryLabel: "处理中", primaryDisabled: true });
  }
}

function clearPacketDisplay(): void {
  currentPacket = undefined;
  clearDirectPreview();
  endpoints.hidden = true;
  apiOrigin.textContent = "—";
  targetOrigin.textContent = "—";
  steps.replaceChildren();
}

async function refreshCapturePageOrigin(): Promise<void> {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  capturePageOrigin = httpOriginFromURL(tab?.url);
  currentOrigin = capturePageOrigin;
}

function renderActiveContextResponse(response: WorkerResponse): void {
  if (response.packet) renderPacket(response.packet);
  else if (response.capture) renderCapture(response.capture);
  else if (response.mode === "binding") {
    clearPacketDisplay();
    endpoints.hidden = false;
    apiOrigin.textContent = response.origin ?? "—";
    targetOrigin.textContent = "Octopus 已初始化";
    renderView({ status: "Octopus 已连接", detail: "管理员 JWT 已验证并仅保存在本机。日常请在中转站页面点击扩展。", primaryAction: null, primaryLabel: "已初始化", primaryDisabled: true });
  } else if (response.mode === "direct" && response.message === "credential_generation") {
    clearPacketDisplay();
    endpoints.hidden = false;
    targetOrigin.textContent = capturePageOrigin ?? "—";
    renderView({ status: "需要系统令牌", detail: "已确认登录，但页面没有完整系统访问令牌。生成操作会覆盖旧系统令牌，且本次最多执行一次。", primaryAction: "generate_direct", primaryLabel: "生成系统令牌并继续", primaryDisabled: false });
    showManualTokenInput();
  } else {
    clearPacketDisplay();
    renderView(waitingView(response.message));
  }
}

async function refreshActiveContext(): Promise<void> {
  await refreshCapturePageOrigin();
  renderActiveContextResponse(await send({ type: "get_active_context", origin: capturePageOrigin }));
}

async function captureActiveSession(): Promise<void> {
  const expectedOrigin = capturePageOrigin;
  if (!expectedOrigin) return renderView(waitingView("当前标签页不是可访问的 HTTP(S) 页面。"));
  renderView(readingView());
  try {
    const response = await runWithRoutedPagePermission(expectedOrigin, () => send({ type: "capture_active_session", expected_origin: expectedOrigin }));
    renderActiveContextResponse(response);
  } catch (error) {
    renderView(waitingView(error instanceof Error ? error.message : "无法读取当前标签页"));
  }
}

async function grantAndOpen(): Promise<void> {
  const packet = currentPacket;
  if (!packet) return renderView(waitingView("没有活动恢复会话。"));
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
  if (!packet) return renderView(waitingView("没有活动恢复会话。"));
  const canGenerateSystemToken = packet.auth.compatible_family === "new-api";
  renderView(submittingView(canGenerateSystemToken));
  try {
    const response = await runWithTargetPermission(packet.origin, () => send({ type: "extract_and_submit" }));
    const result = response.result;
    if (!response.ok && !result) renderView(retryExtractionView("操作失败", response.message ?? "扩展操作失败", "重新授权并提取"));
    else if (result?.kind === "candidate") { currentPacket = undefined; renderView(candidateDeliveredView()); }
    else if (result?.kind === "manual_required") renderView(retryExtractionView("需要手动创建令牌", result.message, "令牌准备好后重新提取"));
    else if (result?.kind === "not_logged_in") renderView(retryExtractionView("尚未完成登录", result.message, canGenerateSystemToken ? "生成/读取系统令牌并验证" : "我已完成登录，重新提取"));
    else if (result) renderView(retryExtractionView("提取失败", result.message));
  } catch (error) {
    renderView(retryExtractionView("无法访问目标站点", error instanceof Error ? error.message : "无法确认临时权限", "重新授权并提取"));
  }
}

async function runPrimaryAction(): Promise<void> {
  if (primaryAction === "capture") return captureActiveSession();
  if (primaryAction === "grant") return grantAndOpen();
  if (primaryAction === "extract") return extractAndSubmit();
  if (primaryAction === "confirm_rebind") {
    const response = await send({ type: "confirm_binding_replacement" });
    if (!response.ok) renderView(waitingView(response.message)); else void refreshActiveContext();
    return;
  }
  if (!currentOrigin) return;
  if (primaryAction === "generate_direct") {
    renderView({ status: "正在生成并验证", detail: "同一捕获只会调用一次系统令牌生成接口。", primaryAction: null, primaryLabel: "正在处理", primaryDisabled: true });
    const response = await runWithTargetPermission(currentOrigin, () => send({ type: "generate_direct_token", origin: currentOrigin! }));
    const resultMessage = response.result && response.result.kind !== "candidate" ? response.result.message : undefined;
    if (response.capture) renderCapture(response.capture); else renderView(waitingView(response.message ?? resultMessage));
    return;
  }
  if (!currentCapture) return;
  if (primaryAction === "confirm_direct") {
    const response = await send({ type: "confirm_direct_capture", origin: currentOrigin, capture_id: currentCapture.capture_id, preview_version: currentCapture.preview_version ?? "", site_name: siteNameInput.value.trim() || undefined, account_name: accountNameInput.value.trim() || undefined });
    if (response.capture) renderCapture(response.capture); else renderView(waitingView(response.message));
  } else if (primaryAction === "resolve_direct") {
    const accountID = Number(accountResolution.value);
    const response = await send({ type: "resolve_direct_capture", origin: currentOrigin, capture_id: currentCapture.capture_id, account_id: Number.isSafeInteger(accountID) ? accountID : undefined });
    if (response.capture) renderCapture(response.capture); else renderView(waitingView(response.message));
  } else if (primaryAction === "retry_sync") {
    const response = await send({ type: "retry_direct_sync", origin: currentOrigin, capture_id: currentCapture.capture_id });
    if (response.capture) renderCapture(response.capture); else renderView(waitingView(response.message));
  }
}

primaryButton.addEventListener("click", () => { void runPrimaryAction(); });
secondaryButton.addEventListener("click", () => {
  if (!currentCapture || !currentOrigin) return;
  void send({ type: "resolve_direct_capture", origin: currentOrigin, capture_id: currentCapture.capture_id, create_new: true }).then((response) => {
    if (response.capture) renderCapture(response.capture); else renderView(waitingView(response.message));
  });
});

discardButton.addEventListener("click", async () => {
  if (currentCapture && currentOrigin) {
    const response = await send({ type: "cancel_direct_capture", origin: currentOrigin, capture_id: currentCapture.capture_id });
    if (response.capture) renderCapture(response.capture);
    return;
  }
  await send({ type: "discard_session" });
  clearPacketDisplay();
  renderView(waitingView("会话和临时站点权限已清理。"));
});

chrome.runtime.onMessage.addListener((event: SessionEvent) => {
  if (event.type === "session_updated") renderPacket(event.packet);
  else if (event.type === "session_error") renderView(waitingView(event.message));
  else if (event.type === "binding_updated") void refreshActiveContext();
  else if (event.type === "binding_replacement_required") {
    clearPacketDisplay();
    renderView({ status: "确认更换 Octopus", detail: `当前：${event.current_origin}\n新地址：${event.next_origin}\n换绑后旧实例中的未完成捕获将被清理。`, primaryAction: "confirm_rebind", primaryLabel: "确认更换 Octopus", primaryDisabled: false });
  } else if (event.type === "direct_capture_updated" && event.origin === currentOrigin) renderCapture(event.capture);
  else if (event.type === "direct_capture_manual_required" && event.origin === currentOrigin) {
    renderView({ status: "需要系统令牌", detail: event.message, primaryAction: "generate_direct", primaryLabel: "生成系统令牌并继续", primaryDisabled: false });
    showManualTokenInput();
  }
});

manualTokenButton.addEventListener("click", async () => {
  const token = manualTokenInput.value.trim();
  if (!currentOrigin || !token) {
    renderView(waitingView("请先填写该平台的推荐访问令牌。"));
    return;
  }
  manualTokenButton.disabled = true;
  try {
    const response = await send({ type: "submit_direct_token", origin: currentOrigin, access_token: token });
    if (response.capture) renderCapture(response.capture);
    else renderView(waitingView(response.message));
  } finally {
    manualTokenInput.value = "";
    manualTokenButton.disabled = false;
  }
});

copyDiagnosticsButton.addEventListener("click", async () => {
  const items = await listDiagnostics();
  if (items.length === 0) {
    renderView(waitingView("当前没有可复制的诊断记录。"));
    return;
  }
  await navigator.clipboard.writeText(formatDiagnostics(items));
  renderView(waitingView(`已复制 ${items.length} 条脱敏诊断记录。`));
});

clearDiagnosticsButton.addEventListener("click", async () => {
  await clearDiagnostics();
  renderView(waitingView("脱敏诊断记录已清空。"));
});

chrome.tabs.onActivated.addListener(() => { void refreshActiveContext(); });
chrome.tabs.onUpdated.addListener((_tabId, changeInfo, tab) => {
  if (tab.active && changeInfo.url) void refreshActiveContext();
});

renderView(readingView());
void refreshActiveContext();
