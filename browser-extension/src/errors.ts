const RECOVERY_ERROR_MESSAGES: Record<string, string> = {
  "site.recovery.not_found": "恢复会话不存在或已经过期",
  "site.recovery.expired": "恢复会话已经过期",
  "site.recovery.invalid": "恢复候选数据无效",
  "site.recovery.forbidden": "恢复会话校验失败",
  "site.recovery.replay": "恢复 capability 已经使用",
  "site.recovery.conflict": "账号或恢复会话已经发生变化",
  "site.recovery.rate_limited": "恢复请求过于频繁，请稍后重试",
};

export function recoveryCandidateFailureMessage(errorCode: unknown, status: number): string {
  if (typeof errorCode === "string") {
    const translated = RECOVERY_ERROR_MESSAGES[errorCode.trim()];
    if (translated) return translated;
  }
  return `Octopus 验证失败（HTTP ${status}）`;
}
