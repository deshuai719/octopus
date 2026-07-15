export type PanelPrimaryAction =
  | "capture"
  | "generate_direct"
  | "confirm_direct"
  | "resolve_direct"
  | "retry_sync"
  | "confirm_rebind"
  | null;

export type PanelView = {
  status: string;
  detail: string;
  primaryAction: PanelPrimaryAction;
  primaryLabel: string;
  primaryDisabled: boolean;
};

export function waitingView(detail = "请在已登录的 Octopus 管理员页面或受支持中转站页面读取当前标签页。"): PanelView {
  return {
    status: "准备连接 Octopus",
    detail,
    primaryAction: "capture",
    primaryLabel: "读取当前标签页",
    primaryDisabled: false,
  };
}

export function readingView(): PanelView {
  return {
    status: "正在读取当前页面",
    detail: "正在识别 Octopus 或中转站登录状态。",
    primaryAction: null,
    primaryLabel: "正在读取",
    primaryDisabled: true,
  };
}

export function submittingView(canGenerateSystemToken = false): PanelView {
  return {
    status: canGenerateSystemToken ? "正在生成/读取并验证" : "正在提取并验证",
    detail: canGenerateSystemToken
      ? "已有完整令牌时只读取；缺少时本次直接捕获最多自动生成一次。"
      : "只会读取当前平台允许的凭据字段。",
    primaryAction: null,
    primaryLabel: canGenerateSystemToken ? "正在生成/读取并验证" : "正在提取并验证",
    primaryDisabled: true,
  };
}
