import type { DirectCaptureView } from "./types";

export type TransferProgressState = "pending" | "active" | "complete" | "failed";
export type TransferProgressStep = {
  key: "detected" | "previewed" | "saved" | "synced";
  label: string;
  state: TransferProgressState;
  status: string;
};

export type CaptureReceiptView = {
  action: string;
  site: string;
  account: string;
  saved: string;
  synced: string;
  message: string;
};

const stepDefinitions: Array<Pick<TransferProgressStep, "key" | "label">> = [
  { key: "detected", label: "识别站点" },
  { key: "previewed", label: "预览已送达" },
  { key: "saved", label: "账号已保存" },
  { key: "synced", label: "同步完成" },
];

const stateLabel: Record<TransferProgressState, string> = {
  pending: "等待中",
  active: "进行中",
  complete: "已完成",
  failed: "失败",
};

const steps = (states: TransferProgressState[]): TransferProgressStep[] =>
  stepDefinitions.map((step, index) => ({ ...step, state: states[index], status: stateLabel[states[index]] }));

export function captureActionLabel(action: string | undefined): string {
  switch (action) {
    case "create_site_account": return "新建站点和账号";
    case "create_account": return "为已有站点新增账号";
    case "update_account": return "更新已有账号";
    default: return "等待确认";
  }
}

export function transferProgressForPhase(phase: string): TransferProgressStep[] {
  switch (phase) {
    case "reading":
      return steps(["active", "pending", "pending", "pending"]);
    case "capture_failed":
      return steps(["failed", "pending", "pending", "pending"]);
    case "resolution_required":
    case "preview_ready":
    case "canceled":
      return steps(["complete", "complete", "pending", "pending"]);
    case "saving":
      return steps(["complete", "complete", "active", "pending"]);
    case "save_failed":
      return steps(["complete", "complete", "failed", "pending"]);
    case "saved_syncing":
    case "retrying_sync":
      return steps(["complete", "complete", "complete", "active"]);
    case "sync_failed":
      return steps(["complete", "complete", "complete", "failed"]);
    case "completed":
      return steps(["complete", "complete", "complete", "complete"]);
    case "conflict":
    case "expired":
      return steps(["complete", "complete", "failed", "pending"]);
    case "failed":
      return steps(["complete", "failed", "pending", "pending"]);
    default:
      return steps(["complete", "active", "pending", "pending"]);
  }
}

const namedID = (name: string | undefined, id: number | undefined, fallback: string): string => {
  if (name && id) return `${name} · ID ${id}`;
  if (name) return name;
  if (id) return `${fallback} ID ${id}`;
  return "—";
};

const syncStatusLabel = (capture: DirectCaptureView): string => {
  if (capture.phase === "saved_syncing") return "正在同步";
  if (capture.phase === "sync_failed") return "同步失败，可重试";
  switch (capture.sync_result?.status) {
    case "success": return "同步成功";
    case "partial": return "部分同步完成";
    case "failed": return "同步失败";
    default: return "同步完成";
  }
};

export function captureReceiptFor(capture: DirectCaptureView): CaptureReceiptView | undefined {
  if (!["saved_syncing", "sync_failed", "completed"].includes(capture.phase)) return undefined;
  return {
    action: captureActionLabel(capture.saved?.action ?? capture.action),
    site: namedID(capture.site_name, capture.saved?.site_id ?? capture.site_id, "站点"),
    account: namedID(capture.account_name, capture.saved?.account_id ?? capture.account_id, "账号"),
    saved: "已写入 Octopus",
    synced: syncStatusLabel(capture),
    message:
      capture.sync_result?.message ??
      capture.error_message ??
      (capture.phase === "saved_syncing"
        ? "账号和凭据已保存，正在后台同步站点数据。"
        : capture.phase === "sync_failed"
          ? "账号已经保存；只有同步失败，重试不会再次写入凭据。"
          : "账号保存和完整同步均已完成。"),
  };
}
