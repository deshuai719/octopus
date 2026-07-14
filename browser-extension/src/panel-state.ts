export type PanelPrimaryAction =
  | "capture"
  | "grant"
  | "extract"
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

export function waitingView(detail = "请在 Octopus 恢复页面点击工具栏中的扩展图标。"): PanelView {
  return {
    status: "等待 Octopus 会话",
    detail,
    primaryAction: "capture",
    primaryLabel: "读取当前标签页",
    primaryDisabled: false,
  };
}

export function readingView(): PanelView {
  return {
    status: "正在读取当前页面",
    detail: "正在查找 Octopus 恢复会话。",
    primaryAction: null,
    primaryLabel: "正在读取",
    primaryDisabled: true,
  };
}

export function readyView(detail: string): PanelView {
  return {
    status: "会话已就绪",
    detail,
    primaryAction: "grant",
    primaryLabel: "允许访问并打开登录页",
    primaryDisabled: false,
  };
}

export function requestingPermissionView(): PanelView {
  return {
    status: "正在请求临时权限",
    detail: "请在 Chrome 权限提示中允许访问目标站点。",
    primaryAction: null,
    primaryLabel: "等待授权",
    primaryDisabled: true,
  };
}

export function loginRequiredView(canGenerateSystemToken = false): PanelView {
  return {
    status: "等待完成登录",
    detail: canGenerateSystemToken
      ? "完成登录后点击下方按钮；页面没有完整系统令牌时会自动生成，并可能更新旧系统令牌。"
      : "在新标签页完成登录后，直接点击下方按钮。",
    primaryAction: "extract",
    primaryLabel: canGenerateSystemToken
      ? "生成/读取系统令牌并验证"
      : "我已完成登录，提取并验证",
    primaryDisabled: false,
  };
}

export function submittingView(canGenerateSystemToken = false): PanelView {
  return {
    status: canGenerateSystemToken ? "正在生成/读取并验证" : "正在提取并验证",
    detail: canGenerateSystemToken
      ? "已有完整令牌时只读取；缺少时本恢复会话最多自动生成一次。"
      : "只会读取当前平台允许的凭据字段。",
    primaryAction: null,
    primaryLabel: canGenerateSystemToken ? "正在生成/读取并验证" : "正在提取并验证",
    primaryDisabled: true,
  };
}

export function candidateDeliveredView(): PanelView {
  return {
    status: "候选凭据已送达",
    detail: "回到 Octopus 查看掩码摘要，确认后才会保存。临时权限已撤销。",
    primaryAction: null,
    primaryLabel: "候选凭据已送达",
    primaryDisabled: true,
  };
}

export function retryExtractionView(status: string, detail: string, label = "重新提取并验证"): PanelView {
  return {
    status,
    detail,
    primaryAction: "extract",
    primaryLabel: label,
    primaryDisabled: false,
  };
}
