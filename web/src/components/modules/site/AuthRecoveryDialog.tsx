"use client";

import { useMemo, useState } from "react";
import { CheckCircle2, CircleAlert, ExternalLink, Loader2, ShieldCheck } from "lucide-react";
import {
  type RecoverySession,
  type Site,
  type SiteAccount,
  useCancelSiteAuthRecovery,
  useConfirmSiteAuthRecovery,
  useCreateSiteAuthRecovery,
  useSiteAuthRecovery,
} from "@/api/endpoints/site";
import { translateApiErrorCode } from "@/api/error-i18n";
import { toast } from "@/components/common/Toast";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  site: Site | null;
  account: SiteAccount | null;
  onManualFallback: () => void;
};

function errorMessage(error: unknown) {
  if (error && typeof error === "object" && "message" in error && typeof error.message === "string") {
    return error.message;
  }
  return "操作失败，请稍后重试。";
}

function notifyExtensionTerminal(sessionId: string) {
  if (typeof window === "undefined") return;
  window.postMessage(
    {
      source: "octopus",
      type: "octopus_auth_recovery_terminal",
      session_id: sessionId,
    },
    window.location.origin,
  );
}

export function AuthRecoveryDialog({
  open,
  onOpenChange,
  site,
  account,
  onManualFallback,
}: Props) {
  const [created, setCreated] = useState<RecoverySession | null>(null);
  const createRecovery = useCreateSiteAuthRecovery();
  const recoveryQuery = useSiteAuthRecovery(created?.id ?? null, open && Boolean(created));
  const confirmRecovery = useConfirmSiteAuthRecovery();
  const cancelRecovery = useCancelSiteAuthRecovery();
  const session = useMemo(() => {
    if (!recoveryQuery.data) return created;
    const keepCapability =
      recoveryQuery.data.phase === "awaiting_login" ||
      recoveryQuery.data.phase === "validating";
    return {
      ...recoveryQuery.data,
      capability: keepCapability ? created?.capability : undefined,
    };
  }, [created, recoveryQuery.data]);

  const extensionPacket = useMemo(() => {
    if (!session?.capability || typeof window === "undefined") return null;
    return {
      version: 1,
      api_base_url: window.location.origin,
      session_id: session.id,
      capability: session.capability,
      account_id: session.account_id,
      site_id: session.site_id,
      origin: session.origin,
      platform: session.platform,
      expires_at: session.expires_at,
      auth: session.auth,
    };
  }, [session]);

  async function startRecovery() {
    if (!account) return;
    try {
      const next = await createRecovery.mutateAsync(account.id);
      setCreated(next);
      toast.success("恢复会话已创建，请点击浏览器工具栏中的 Octopus 登录助手");
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  async function confirm() {
    if (!session) return;
    try {
      await confirmRecovery.mutateAsync(session.id);
      notifyExtensionTerminal(session.id);
      toast.success("登录凭据已验证并保存，账号同步已恢复");
      setCreated(null);
      onOpenChange(false);
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  async function cancel() {
    if (!session) {
      setCreated(null);
      onOpenChange(false);
      return;
    }
    try {
      await cancelRecovery.mutateAsync(session.id);
      notifyExtensionTerminal(session.id);
      setCreated(null);
      onOpenChange(false);
    } catch (error) {
      toast.error(errorMessage(error));
    }
  }

  async function manualFallback() {
    if (session) {
      try {
        await cancelRecovery.mutateAsync(session.id);
        notifyExtensionTerminal(session.id);
      } catch (error) {
        toast.error(errorMessage(error));
        return;
      }
    }
    setCreated(null);
    onManualFallback();
  }

  function handleOpenChange(nextOpen: boolean) {
    if (!nextOpen && session && session.phase !== "completed" && session.phase !== "canceled") {
      void cancel();
      return;
    }
    onOpenChange(nextOpen);
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-xl rounded-3xl">
        <DialogHeader>
          <DialogTitle>修复 {account?.name ?? "站点账号"} 的登录状态</DialogTitle>
          <DialogDescription>
            浏览器扩展只会读取目标站点允许的凭据字段；确认前不会覆盖现有凭据或渠道。
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 text-sm">
          {!session ? (
            <div className="rounded-2xl border bg-muted/20 p-4">
              <p className="font-medium">准备启动本机浏览器辅助登录</p>
              <p className="mt-1 text-muted-foreground">
                你仍需亲自完成密码、验证码、二次验证和 Cloudflare。扩展不会读取密码，也不会绕过安全验证。
              </p>
            </div>
          ) : (
            <>
              {extensionPacket ? (
                <script
                  type="application/json"
                  data-octopus-recovery="true"
                  dangerouslySetInnerHTML={{ __html: JSON.stringify(extensionPacket).replaceAll("<", "\\u003c") }}
                />
              ) : null}

              <div className="rounded-2xl border p-4">
                <div className="flex items-center gap-2 font-medium">
                  {session.phase === "candidate_ready" ? (
                    <CheckCircle2 className="size-4 text-emerald-600" />
                  ) : session.phase === "verification_failed" ? (
                    <CircleAlert className="size-4 text-destructive" />
                  ) : (
                    <Loader2 className="size-4 animate-spin text-primary" />
                  )}
                  {session.phase === "awaiting_login" && "等待扩展读取会话"}
                  {session.phase === "validating" && "正在服务端验证候选凭据"}
                  {session.phase === "candidate_ready" && "候选凭据验证成功"}
                  {session.phase === "verification_failed" && "候选凭据验证失败"}
                  {session.phase === "confirming" && "正在提交恢复结果"}
                  {session.phase === "completed" && "账号已恢复"}
                  {session.phase === "canceled" && "恢复已取消"}
                </div>
                <p className="mt-2 text-xs text-muted-foreground">
                  会话将在 {new Date(session.expires_at).toLocaleString()} 失效。请点击浏览器工具栏中的扩展图标读取本页会话。
                </p>
              </div>

              <div className="rounded-2xl border p-4">
                <div className="flex items-center gap-2 font-medium">
                  <ShieldCheck className="size-4 text-primary" />
                  {session.auth.recovery_guide.title}
                </div>
                <ol className="mt-3 list-decimal space-y-1 pl-5 text-muted-foreground">
                  {session.auth.recovery_guide.steps.map((step) => (
                    <li key={step}>{step}</li>
                  ))}
                </ol>
                <Button asChild variant="outline" className="mt-3 rounded-xl">
                  <a href={session.origin} target="_blank" rel="noreferrer">
                    打开目标站点
                    <ExternalLink className="size-4" />
                  </a>
                </Button>
              </div>

              {session.candidate ? (
                <div className="rounded-2xl border border-emerald-500/30 bg-emerald-500/5 p-4">
                  <p className="font-medium">待保存凭据摘要</p>
                  <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-muted-foreground">
                    <dt>Access Token</dt>
                    <dd className="font-mono">{session.candidate.access_token_mask}</dd>
                    <dt>Refresh Token</dt>
                    <dd>{session.candidate.has_refresh_token ? "已提取" : "无"}</dd>
                    <dt>用户 ID</dt>
                    <dd>{session.candidate.platform_user_id ?? "无"}</dd>
                  </dl>
                  {session.candidate.identity_changed ? (
                    <p className="mt-3 rounded-xl bg-amber-500/10 px-3 py-2 text-amber-700 dark:text-amber-300">
                      扩展识别到的用户 ID 与原账号不同，请确认你登录的是正确账号。
                    </p>
                  ) : null}
                </div>
              ) : null}

              {session.error_code ? (
                <p className="rounded-xl bg-destructive/10 px-3 py-2 text-destructive">
                  {translateApiErrorCode(session.error_code, "恢复失败，请重新创建恢复会话。")}
                </p>
              ) : null}
            </>
          )}

          <button
            type="button"
            className="text-left text-sm text-primary underline-offset-4 hover:underline"
            onClick={manualFallback}
          >
            扩展不可用？改为手动填写凭据
          </button>
          {session ? (
            <p className="text-xs text-muted-foreground">
              {session.auth.recovery_guide.manual_fallback}
            </p>
          ) : null}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={cancel} disabled={cancelRecovery.isPending}>
            取消
          </Button>
          {!session ? (
            <Button onClick={startRecovery} disabled={createRecovery.isPending || !site || !account}>
              {createRecovery.isPending ? "正在创建…" : "创建扩展会话"}
            </Button>
          ) : (
            <Button
              onClick={confirm}
              disabled={session.phase !== "candidate_ready" || confirmRecovery.isPending}
            >
              {confirmRecovery.isPending ? "正在确认…" : "确认保存并同步"}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
