import { canonicalHTTPOrigin } from "./binding";
import { runWithRoutedPagePermission, runWithTargetPermission } from "./permissions";
import { clearDiagnostics, formatDiagnostics, listDiagnostics } from "./diagnostics";
import { readingView, submittingView, waitingView } from "./panel-state";
import type { PanelPrimaryAction, PanelView } from "./panel-state";
import { captureActionLabel, captureReceiptFor, transferProgressForPhase } from "./capture-progress";
import {
  UPDATE_STATE_KEY,
  type ExtensionUpdateState,
  loadUpdateState,
  patchUpdateState,
  manualInstallerPending,
  prepareBundledUpdaterDownload,
} from "./updater";
import type { DirectCaptureSummaryItem, DirectCaptureView, SessionEvent, WorkerRequest, WorkerResponse } from "./types";

const status = document.querySelector<HTMLElement>("#status")!;
const detail = document.querySelector<HTMLElement>("#detail")!;
const endpoints = document.querySelector<HTMLElement>("#endpoints")!;
const apiOrigin = document.querySelector<HTMLElement>("#api-origin")!;
const targetOrigin = document.querySelector<HTMLElement>("#target-origin")!;
const primaryButton = document.querySelector<HTMLButtonElement>("#primary-action")!;
const secondaryButton = document.querySelector<HTMLButtonElement>("#secondary-action")!;
const discardButton = document.querySelector<HTMLButtonElement>("#discard")!;
const copyDiagnosticsButton = document.querySelector<HTMLButtonElement>("#copy-diagnostics")!;
const clearDiagnosticsButton = document.querySelector<HTMLButtonElement>("#clear-diagnostics")!;
const directPreview = document.querySelector<HTMLElement>("#direct-preview")!;
const transferProgress = document.querySelector<HTMLElement>("#transfer-progress")!;
const transferProgressItems = new Map(
  Array.from(transferProgress.querySelectorAll<HTMLElement>("[data-progress-step]"))
    .map((item): [string, HTMLElement] => [item.dataset.progressStep ?? "", item]),
);
const transferReceipt = document.querySelector<HTMLElement>("#transfer-receipt")!;
const receiptAction = document.querySelector<HTMLElement>("#receipt-action")!;
const receiptSite = document.querySelector<HTMLElement>("#receipt-site")!;
const receiptAccount = document.querySelector<HTMLElement>("#receipt-account")!;
const receiptSaved = document.querySelector<HTMLElement>("#receipt-saved")!;
const receiptSynced = document.querySelector<HTMLElement>("#receipt-synced")!;
const receiptMessage = document.querySelector<HTMLElement>("#receipt-message")!;
const captureAction = document.querySelector<HTMLElement>("#capture-action")!;
const credentialMask = document.querySelector<HTMLElement>("#credential-mask")!;
const platformUserID = document.querySelector<HTMLElement>("#platform-user-id")!;
const matchedSiteRow = document.querySelector<HTMLElement>("#matched-site-row")!;
const matchedSite = document.querySelector<HTMLElement>("#matched-site")!;
const matchedAccountRow = document.querySelector<HTMLElement>("#matched-account-row")!;
const matchedAccount = document.querySelector<HTMLElement>("#matched-account")!;
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
const siteTagsField = document.querySelector<HTMLElement>("#site-tags-field")!;
const siteBillingTag = document.querySelector<HTMLSelectElement>("#site-billing-tag")!;
const siteCustomTags = document.querySelector<HTMLInputElement>("#site-custom-tags")!;
const siteExistingTags = document.querySelector<HTMLElement>("#site-existing-tags")!;
const summaryCounts = document.querySelector<HTMLElement>("#summary-counts")!;
const summaryList = document.querySelector<HTMLElement>("#summary-list")!;
const refreshSummaryButton = document.querySelector<HTMLButtonElement>("#refresh-summary")!;
const extensionUpdate = document.querySelector<HTMLElement>("#extension-update")!;
const updateCurrentVersion = document.querySelector<HTMLElement>("#update-current-version")!;
const updateBadge = document.querySelector<HTMLElement>("#update-badge")!;
const updateMessage = document.querySelector<HTMLElement>("#update-message")!;
const updateVersionRow = document.querySelector<HTMLElement>("#update-version-row")!;
const updateLatestVersion = document.querySelector<HTMLElement>("#update-latest-version")!;
const updateTargets = document.querySelector<HTMLElement>("#update-targets")!;
const checkExtensionUpdateButton = document.querySelector<HTMLButtonElement>("#check-extension-update")!;
const runExtensionUpdateButton = document.querySelector<HTMLButtonElement>("#run-extension-update")!;
const selectExtensionTargetButton = document.querySelector<HTMLButtonElement>("#select-extension-target")!;
const rollbackExtensionUpdateButton = document.querySelector<HTMLButtonElement>("#rollback-extension-update")!;

let currentCapture: DirectCaptureView | undefined;
let currentOrigin: string | undefined;
let primaryAction: PanelPrimaryAction = "capture";
let capturePageOrigin: string | undefined;
let summaryTimer: ReturnType<typeof setTimeout> | undefined;
let summaryGeneration = 0;
let summaryActiveSince = 0;
let storageRefreshTimer: ReturnType<typeof setTimeout> | undefined;

function send(request: WorkerRequest) {
  return chrome.runtime.sendMessage<WorkerRequest, WorkerResponse>(request);
}

function updatePhaseLabel(phase: ExtensionUpdateState["phase"]): string {
  return ({
    idle: "未检查",
    checking: "检查中",
    host_required: "需启用",
    target_required: "需选目录",
    ready: "已是最新",
    update_available: "发现新版",
    installer_downloading: "准备中",
    installer_ready: "待手动运行",
    installing: "待检测",
    updating: "更新中",
    updated: "已更新",
    rolling_back: "回滚中",
    rolled_back: "已回滚",
    error: "失败",
  } as Record<ExtensionUpdateState["phase"], string>)[phase];
}

function renderUpdateState(state: ExtensionUpdateState): void {
  extensionUpdate.dataset.phase = state.phase;
  updateCurrentVersion.textContent = state.current_version;
  updateBadge.textContent = updatePhaseLabel(state.phase);
  updateMessage.textContent = state.message;
  updateVersionRow.hidden = !state.latest_version;
  updateLatestVersion.textContent = state.latest_version ?? "—";
  updateTargets.hidden = state.targets.length === 0;
  updateTargets.replaceChildren(...state.targets.map((target) => {
    const item = document.createElement("div");
    item.className = "update-target";
    const label = document.createElement("strong");
    label.textContent = `${target.browser} · ${target.profile}${target.status ? ` · ${target.status}` : ""}`;
    const path = document.createElement("code");
    path.textContent = target.path;
    item.append(label, path);
    return item;
  }));

  checkExtensionUpdateButton.disabled = ["checking", "updating", "rolling_back", "installer_downloading"].includes(state.phase);
  selectExtensionTargetButton.hidden = state.phase !== "target_required";
  rollbackExtensionUpdateButton.hidden = !["updated", "rolled_back", "error"].includes(state.phase) || state.targets.length === 0;
  runExtensionUpdateButton.hidden = false;
  runExtensionUpdateButton.disabled = false;
  if (state.phase === "host_required") runExtensionUpdateButton.textContent = "下载更新助手";
  else if (state.phase === "installer_downloading") {
    runExtensionUpdateButton.textContent = "正在准备";
    runExtensionUpdateButton.disabled = true;
  } else if (state.phase === "installer_ready") runExtensionUpdateButton.textContent = "检测助手";
  else if (state.phase === "installing") runExtensionUpdateButton.textContent = "检测助手";
  else if (state.phase === "target_required") runExtensionUpdateButton.textContent = "选择扩展目录";
  else if (state.phase === "update_available") runExtensionUpdateButton.textContent = `更新到 ${state.latest_version}`;
  else if (state.phase === "updating") {
    runExtensionUpdateButton.textContent = "正在更新";
    runExtensionUpdateButton.disabled = true;
  } else if (state.phase === "error" && state.error_code === "updater.host.unavailable") runExtensionUpdateButton.textContent = "重新下载助手";
  else {
    runExtensionUpdateButton.textContent = "立即更新";
    runExtensionUpdateButton.disabled = !state.update_available;
  }
}

async function refreshUpdateState(check = false): Promise<void> {
  const response = await send(check ? { type: "check_extension_update" } : { type: "get_extension_update_state" });
  if (response.update) renderUpdateState(response.update);
}


function renderView(view: PanelView): void {
  status.textContent = view.status;
  detail.textContent = view.detail;
  primaryAction = view.primaryAction;
  primaryButton.textContent = view.primaryLabel;
  primaryButton.disabled = view.primaryDisabled;
  secondaryButton.hidden = true;
}

function clearTransferProgress(): void {
  transferProgress.hidden = true;
  for (const item of transferProgressItems.values()) {
    item.dataset.state = "pending";
    item.removeAttribute("aria-current");
    const statusText = item.querySelector<HTMLElement>("small");
    if (statusText) statusText.textContent = "等待中";
  }
}

function renderTransferProgress(phase: string): void {
  transferProgress.hidden = false;
  for (const step of transferProgressForPhase(phase)) {
    const item = transferProgressItems.get(step.key);
    if (!item) continue;
    item.dataset.state = step.state;
    if (step.state === "active") item.setAttribute("aria-current", "step");
    else item.removeAttribute("aria-current");
    const label = item.querySelector<HTMLElement>("strong");
    const statusText = item.querySelector<HTMLElement>("small");
    if (label) label.textContent = step.label;
    if (statusText) statusText.textContent = step.status;
  }
}

function renderCaptureFailure(message?: string): void {
  clearDirectPreview();
  renderTransferProgress("capture_failed");
  renderView({
    status: "传输未完成",
    detail: message ?? "站点识别或预览验证失败，本次没有保存账号或凭据。",
    primaryAction: "capture",
    primaryLabel: "重新读取当前标签页",
    primaryDisabled: false,
  });
}

function clearTransferReceipt(): void {
  transferReceipt.hidden = true;
  transferReceipt.dataset.state = "";
  receiptAction.textContent = "—";
  receiptSite.textContent = "—";
  receiptAccount.textContent = "—";
  receiptSaved.textContent = "—";
  receiptSynced.textContent = "—";
  receiptMessage.textContent = "";
}

function renderTransferReceipt(capture: DirectCaptureView): void {
  const receipt = captureReceiptFor(capture);
  if (!receipt) return clearTransferReceipt();
  transferReceipt.hidden = false;
  transferReceipt.dataset.state = capture.phase === "sync_failed" ? "failed" : capture.phase === "completed" ? "complete" : "active";
  receiptAction.textContent = receipt.action;
  receiptSite.textContent = receipt.site;
  receiptAccount.textContent = receipt.account;
  receiptSaved.textContent = receipt.saved;
  receiptSynced.textContent = receipt.synced;
  receiptMessage.textContent = receipt.message;
}

function clearDirectPreview(): void {
  currentCapture = undefined;
  directPreview.hidden = true;
  clearTransferProgress();
  clearTransferReceipt();
  siteNameField.hidden = true;
  accountNameField.hidden = true;
  accountResolutionField.hidden = true;
  captureWarning.hidden = true;
  matchedSiteRow.hidden = true;
  matchedAccountRow.hidden = true;
  matchedSite.textContent = "—";
  matchedAccount.textContent = "—";
  siteNameInput.value = "";
  accountNameInput.value = "";
  accountResolution.replaceChildren();
  manualTokenInput.value = "";
  manualTokenField.hidden = true;
  manualTokenButton.hidden = true;
  siteTagsField.hidden = true;
  siteBillingTag.value = "公益";
  siteCustomTags.value = "";
  siteExistingTags.textContent = "";
}

function showManualTokenInput(): void {
  manualTokenField.hidden = false;
  manualTokenButton.hidden = false;
}

function renderCapture(capture: DirectCaptureView): void {
  currentCapture = capture;
  currentOrigin = capture.origin || currentOrigin;
  endpoints.hidden = false;
  targetOrigin.textContent = capture.origin;
  directPreview.hidden = false;
  captureAction.textContent = captureActionLabel(capture.action);
  credentialMask.textContent = capture.candidate?.access_token_mask ?? "已在确认后清除";
  platformUserID.textContent = capture.candidate?.platform_user_id?.toString() ?? "未提供";
  siteNameInput.value = capture.site_name ?? "";
  accountNameInput.value = capture.page_title || capture.account_name || capture.candidate?.identity_label || "默认账号";
  matchedSite.textContent = capture.site_name || "—";
  matchedSiteRow.hidden = !capture.site_name;
  matchedAccount.textContent = capture.account_name || capture.candidate?.identity_label || "—";
  matchedAccountRow.hidden = !(capture.account_name || capture.candidate?.identity_label);
  siteNameField.hidden = capture.action !== "create_site_account";
  accountNameField.hidden = !["create_site_account", "create_account", "update_account"].includes(capture.action ?? "");
  siteTagsField.hidden = capture.phase !== "preview_ready";
  const existingTags = capture.site_tags ?? [];
  siteBillingTag.value = existingTags.includes("付费") ? "付费" : "公益";
  siteCustomTags.value = "";
  siteExistingTags.textContent = existingTags.length > 0 ? `已有标签将保留：${existingTags.join("、")}` : "新站点或未分类站点默认补充“公益”。";
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
  if (capture.tag_update_supported === false) warnings.push("当前 Octopus 后端版本暂不支持导入标签；账号已保存，但标签未随本次导入更新。");
  captureWarning.textContent = warnings.join(" ");
  captureWarning.hidden = warnings.length === 0;
  renderTransferProgress(capture.phase);
  renderTransferReceipt(capture);

  switch (capture.phase) {
    case "resolution_required":
      renderView({ status: "需要选择账号", detail: "当前站点有多个管理账号，请选择要更新的账号，或明确创建新账号。", primaryAction: "resolve_direct", primaryLabel: "使用选择的账号", primaryDisabled: false });
      secondaryButton.hidden = false;
      secondaryButton.textContent = "创建新账号";
      break;
    case "preview_ready":
      renderView({ status: "预览已送达，等待确认", detail: "Octopus 已收到并验证脱敏预览；账号和凭据尚未写入。只有点击确认后才会保存。", primaryAction: "confirm_direct", primaryLabel: "确认创建或更新", primaryDisabled: false });
      break;
    case "saved_syncing":
      renderView({ status: "账号已保存", detail: "凭据已安全保存，正在后台执行完整同步。", primaryAction: null, primaryLabel: "正在同步", primaryDisabled: true });
      break;
    case "sync_failed":
      renderView({ status: "账号已保存，同步失败", detail: capture.error_message ?? "可以主动重试同步，不会再次写入凭据。", primaryAction: "retry_sync", primaryLabel: "重试同步", primaryDisabled: false });
      break;
    case "completed":
      renderView({ status: "创建或更新完成", detail: capture.sync_result?.message ?? "账号保存和完整同步均已完成。可以切换到下一站，或重新读取当前站点。", primaryAction: "capture", primaryLabel: "继续导入或重新读取", primaryDisabled: false });
      break;
    case "canceled":
      renderView(waitingView("本次直接捕获已取消，临时权限已清理。"));
      break;
    case "failed":
    case "conflict":
    case "expired":
      renderView(waitingView(capture.error_message ?? "本次直接捕获已结束，请重新读取当前站点。"));
      break;
    default:
      renderView({ status: "正在处理", detail: capture.error_message ?? `当前阶段：${capture.phase}`, primaryAction: null, primaryLabel: "处理中", primaryDisabled: true });
  }
}

function summaryPhaseLabel(phase: string): string {
  return ({ saved_syncing: "同步中", completed: "成功", sync_failed: "失败", preview_ready: "待确认", resolution_required: "待选择", credential_generation: "待令牌", confirming: "保存中", failed: "失败", conflict: "冲突", canceled: "已取消", expired: "已过期" } as Record<string, string>)[phase] ?? phase;
}

function renderSummary(items: DirectCaptureSummaryItem[]): void {
  const syncing = items.filter((item) => item.phase === "saved_syncing").length;
  const completed = items.filter((item) => item.phase === "completed").length;
  const failed = items.filter((item) => ["sync_failed", "failed", "conflict"].includes(item.phase)).length;
  summaryCounts.textContent = items.length === 0 ? "暂无记录" : `${items.length} 项 · ${syncing} 同步中 · ${completed} 成功 · ${failed} 失败`;
  if (items.length === 0) {
    const empty = document.createElement("p");
    empty.className = "summary-empty";
    empty.textContent = "本次浏览器会话还没有导入记录。";
    summaryList.replaceChildren(empty);
    return;
  }
  summaryList.replaceChildren(...items.map((item) => {
    const card = document.createElement("article");
    card.className = "summary-item";
    card.dataset.phase = item.phase;
    card.dataset.current = String(item.origin === currentOrigin);
    const head = document.createElement("div");
    head.className = "summary-item-head";
    const link = document.createElement("button");
    link.className = "summary-origin";
    link.textContent = item.site_name ? `${item.site_name} · ${item.origin}` : item.origin;
    link.addEventListener("click", () => { void send({ type: "open_direct_capture_origin", origin: item.origin }); });
    const badge = document.createElement("span");
    badge.className = "summary-badge";
    badge.textContent = summaryPhaseLabel(item.phase);
    head.append(link, badge);
    const meta = document.createElement("p");
    meta.className = "summary-meta";
    const elapsed = Math.max(0, Math.floor((Date.now() - Date.parse(item.sync_started_at ?? item.created_at)) / 1000));
    meta.textContent = `${item.saved ? "账号已保存" : "尚未保存"} · ${elapsed}s${item.sync_result?.message ? ` · ${item.sync_result.message}` : item.error_message ? ` · ${item.error_message}` : ""}`;
    const actions = document.createElement("div");
    actions.className = "summary-actions";
    if (item.can_retry_sync && item.capture_id) {
      const retry = document.createElement("button");
      retry.className = "secondary";
      retry.textContent = "重试同步";
      retry.addEventListener("click", async () => {
        retry.disabled = true;
        await send({ type: "retry_direct_sync", origin: item.origin, capture_id: item.capture_id! });
        await refreshSummary(true);
      });
      actions.append(retry);
    }
    if (item.can_clear) {
      const clear = document.createElement("button");
      clear.className = "quiet";
      clear.textContent = "清理";
      clear.addEventListener("click", async () => {
        await send({ type: "clear_direct_session", origin: item.origin });
        await refreshSummary(false);
      });
      actions.append(clear);
    }
    card.append(head, meta, actions);
    return card;
  }));
}

async function refreshSummary(refreshActive = true): Promise<void> {
  const generation = ++summaryGeneration;
  const response = await send({ type: "get_direct_capture_summary", refresh_active: refreshActive });
  if (generation !== summaryGeneration || !response.ok) return;
  const items = response.summary ?? [];
  renderSummary(items);
  if (summaryTimer) clearTimeout(summaryTimer);
  if (items.some((item) => item.phase === "saved_syncing")) {
    if (summaryActiveSince === 0) summaryActiveSince = Date.now();
    summaryTimer = setTimeout(() => { void refreshSummary(true); }, Date.now() - summaryActiveSince < 20_000 ? 2_000 : 5_000);
  } else {
    summaryActiveSince = 0;
    summaryTimer = undefined;
  }
}

function clearContextDisplay(): void {
  clearDirectPreview();
  endpoints.hidden = true;
  apiOrigin.textContent = "—";
  targetOrigin.textContent = "—";
}

async function refreshCapturePageOrigin(): Promise<void> {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  capturePageOrigin = canonicalHTTPOrigin(tab?.url);
  currentOrigin = capturePageOrigin;
}

function renderActiveContextResponse(response: WorkerResponse): void {
  if (!response.ok) {
    renderCaptureFailure(response.message);
    return;
  }
  if (response.capture) {
    if (response.binding?.origin) apiOrigin.textContent = response.binding.origin;
    renderCapture(response.capture);
  }
  else if (response.mode === "binding") {
    clearContextDisplay();
    endpoints.hidden = false;
    apiOrigin.textContent = response.origin ?? "—";
    targetOrigin.textContent = "Octopus 已初始化";
    renderView({ status: "Octopus 已连接", detail: "管理员 JWT 已验证并仅保存在本机。日常请在中转站页面点击扩展。", primaryAction: null, primaryLabel: "已初始化", primaryDisabled: true });
  } else if (response.mode === "direct" && response.message === "credential_generation") {
    clearContextDisplay();
    endpoints.hidden = false;
    targetOrigin.textContent = capturePageOrigin ?? "—";
    renderView({ status: "需要系统令牌", detail: "已确认登录，但页面没有完整系统访问令牌。生成操作会覆盖旧系统令牌，且本次最多执行一次。", primaryAction: "generate_direct", primaryLabel: "生成系统令牌并继续", primaryDisabled: false });
    showManualTokenInput();
  } else {
    clearContextDisplay();
    renderView(waitingView(response.message));
  }
}

async function refreshActiveContext(): Promise<void> {
  await refreshCapturePageOrigin();
  renderActiveContextResponse(await send({ type: "get_active_context", origin: capturePageOrigin }));
  void refreshSummary(false);
}

async function captureActiveSession(): Promise<void> {
  const expectedOrigin = capturePageOrigin;
  if (!expectedOrigin) return renderView(waitingView("当前标签页不是可访问的 HTTP(S) 页面。"));
  clearDirectPreview();
  endpoints.hidden = false;
  targetOrigin.textContent = expectedOrigin;
  renderTransferProgress("reading");
  renderView(readingView());
  try {
    const response = await runWithRoutedPagePermission(expectedOrigin, () => send({ type: "capture_active_session", expected_origin: expectedOrigin }));
    renderActiveContextResponse(response);
  } catch (error) {
    renderCaptureFailure(error instanceof Error ? error.message : "无法读取当前标签页");
  }
}

async function runPrimaryAction(): Promise<void> {
  if (primaryAction === "capture") return captureActiveSession();
  if (primaryAction === "confirm_rebind") {
    const response = await send({ type: "confirm_binding_replacement" });
    if (!response.ok) renderView(waitingView(response.message)); else void refreshActiveContext();
    return;
  }
  if (!currentOrigin) return;
  if (primaryAction === "generate_direct") {
    renderView(submittingView(true));
    const response = await runWithTargetPermission(currentOrigin, () => send({ type: "generate_direct_token", origin: currentOrigin! }));
    const resultMessage = response.result && response.result.kind !== "candidate" ? response.result.message : undefined;
    if (response.capture) renderCapture(response.capture); else renderView(waitingView(response.message ?? resultMessage));
    return;
  }
  if (!currentCapture) return;
  if (primaryAction === "confirm_direct") {
    renderTransferProgress("saving");
    clearTransferReceipt();
    renderView({ status: "正在保存账号", detail: "正在确认写入 Octopus；完成前请勿重复点击。", primaryAction: null, primaryLabel: "正在保存", primaryDisabled: true });
    const customTags = siteCustomTags.value.split(/[,，]/).map((tag) => tag.trim()).filter(Boolean);
    const response = await send({ type: "confirm_direct_capture", origin: currentOrigin, capture_id: currentCapture.capture_id, preview_version: currentCapture.preview_version ?? "", site_name: siteNameInput.value.trim() || undefined, account_name: accountNameInput.value.trim() || undefined, add_tags: [siteBillingTag.value, ...customTags] });
    if (response.capture) renderCapture(response.capture);
    else {
      renderTransferProgress("save_failed");
      renderView(waitingView(response.message));
    }
  } else if (primaryAction === "resolve_direct") {
    const accountID = Number(accountResolution.value);
    const response = await send({ type: "resolve_direct_capture", origin: currentOrigin, capture_id: currentCapture.capture_id, account_id: Number.isSafeInteger(accountID) ? accountID : undefined });
    if (response.capture) renderCapture(response.capture); else renderView(waitingView(response.message));
  } else if (primaryAction === "retry_sync") {
    renderTransferProgress("retrying_sync");
    renderView({ status: "正在重试同步", detail: "账号已经保存；本次只重试同步，不会再次写入凭据。", primaryAction: null, primaryLabel: "正在同步", primaryDisabled: true });
    const response = await send({ type: "retry_direct_sync", origin: currentOrigin, capture_id: currentCapture.capture_id });
    if (response.capture) renderCapture(response.capture);
    else {
      renderTransferProgress("sync_failed");
      renderView(waitingView(response.message));
    }
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
    if (["completed", "sync_failed", "canceled", "failed", "conflict", "expired"].includes(currentCapture.phase)) {
      await send({ type: "clear_direct_session", origin: currentOrigin });
      clearContextDisplay();
      renderView(waitingView("当前站点的临时导入状态已清理，Octopus 绑定保持不变。"));
    } else {
      const response = await send({ type: "cancel_direct_capture", origin: currentOrigin, capture_id: currentCapture.capture_id });
      if (response.capture) renderCapture(response.capture);
    }
    return;
  }
  if (currentOrigin) await send({ type: "clear_direct_session", origin: currentOrigin });
  clearContextDisplay();
  renderView(waitingView("当前站点的临时导入状态已清理，Octopus 绑定保持不变。"));
});

chrome.runtime.onMessage.addListener((event: SessionEvent) => {
  if (event.type === "operation_error") {
    if (!currentCapture && !transferProgress.hidden) renderCaptureFailure(event.message);
    else if (!currentCapture) renderView(waitingView(event.message));
  }
  else if (event.type === "binding_updated") void refreshActiveContext();
  else if (event.type === "binding_replacement_required") {
    clearContextDisplay();
    renderView({ status: "确认更换 Octopus", detail: `当前：${event.current_origin}\n新地址：${event.next_origin}\n换绑后旧实例中的未完成捕获将被清理。`, primaryAction: "confirm_rebind", primaryLabel: "确认更换 Octopus", primaryDisabled: false });
  } else if (event.type === "direct_capture_progress" && event.origin === currentOrigin) {
    renderTransferProgress(event.phase);
    renderView({ status: "站点已识别", detail: "正在提取允许的登录信息并向 Octopus 发送脱敏预览。", primaryAction: null, primaryLabel: "正在送达预览", primaryDisabled: true });
  } else if (event.type === "direct_capture_updated") {
    if (event.origin === currentOrigin) renderCapture(event.capture);
    void refreshSummary(false);
  }
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

refreshSummaryButton.addEventListener("click", () => { void refreshSummary(true); });

checkExtensionUpdateButton.addEventListener("click", async () => {
  const response = await send({ type: "check_extension_update", force: true });
  if (response.update) renderUpdateState(response.update);
});

runExtensionUpdateButton.addEventListener("click", async () => {
  const state = await loadUpdateState();
  if (state.phase === "host_required" || (state.phase === "error" && state.error_code === "updater.host.unavailable")) {
    renderUpdateState(await prepareBundledUpdaterDownload());
    return;
  }
  if (state.phase === "installer_ready" || state.phase === "installing") {
    const response = await send({ type: "refresh_extension_updater_status" });
    if (!response.update) return;
    if (response.update.phase === "host_required") {
      renderUpdateState(await patchUpdateState({
        phase: "installer_ready",
        installer_download_id: state.installer_download_id,
        message: "仍未检测到更新助手。请先从浏览器下载记录中手动运行 octopus-extension-helper.exe，再点击“检测助手”。",
      }));
    } else {
      renderUpdateState(response.update);
      if (response.update.phase === "ready") void refreshUpdateState(true);
    }
    return;
  }
  if (state.phase === "target_required") {
    const response = await send({ type: "select_extension_target" });
    if (response.update) renderUpdateState(response.update);
    return;
  }
  if (state.update_available) {
    const response = await send({ type: "update_extension" });
    if (response.update) renderUpdateState(response.update);
  }
});

selectExtensionTargetButton.addEventListener("click", async () => {
  const response = await send({ type: "select_extension_target" });
  if (response.update) renderUpdateState(response.update);
});

rollbackExtensionUpdateButton.addEventListener("click", async () => {
  const response = await send({ type: "rollback_extension" });
  if (response.update) renderUpdateState(response.update);
});

chrome.storage.onChanged.addListener((changes, areaName) => {
  if (areaName === "local" && UPDATE_STATE_KEY in changes) {
    const state = changes[UPDATE_STATE_KEY].newValue as ExtensionUpdateState | undefined;
    if (state?.version === 1) renderUpdateState(state);
  }
  if (areaName === "session" && "octopusDirectCaptureIndexV1" in changes) {
    if (storageRefreshTimer) clearTimeout(storageRefreshTimer);
    storageRefreshTimer = setTimeout(() => {
      void refreshSummary(false);
      // Dual path: if the main panel is still showing saved_syncing, re-read active capture after session index updates.
      if (currentOrigin && currentCapture?.phase === "saved_syncing") {
        void refreshActiveContext();
      }
    }, 100);
  }
});

chrome.tabs.onActivated.addListener(() => { void refreshActiveContext(); });
chrome.tabs.onUpdated.addListener((_tabId, changeInfo, tab) => {
  if (tab.active && changeInfo.url) void refreshActiveContext();
});

renderView(readingView());
void refreshActiveContext();
void refreshSummary(true);
void loadUpdateState().then((state) => {
  renderUpdateState(state);
  if (!manualInstallerPending(state) && state.phase !== "host_required") {
    void refreshUpdateState(true);
  }
});
