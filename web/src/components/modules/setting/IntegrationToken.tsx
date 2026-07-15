'use client';

import { useState } from 'react';
import { KeyRound, Loader, Plus, RefreshCw, ShieldCheck, Trash2 } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { CopyIconButton } from '@/components/common/CopyButton';
import { toast } from '@/components/common/Toast';
import type { ApiError } from '@/api/types';
import {
    useCreateIntegrationToken,
    useIntegrationTokenList,
    useRevokeIntegrationToken,
    useRotateIntegrationToken,
} from '@/api/endpoints/integration-token';

export function SettingIntegrationToken() {
    const { data: tokens = [], isLoading, isError } = useIntegrationTokenList();
    const createToken = useCreateIntegrationToken();
    const revokeToken = useRevokeIntegrationToken();
    const rotateToken = useRotateIntegrationToken();
    const [name, setName] = useState('Upstream Hub');
    const [rawToken, setRawToken] = useState('');

    const showError = (title: string, error: unknown) => {
        toast.error(title, { description: (error as ApiError)?.message });
    };

    const handleCreate = () => {
        const trimmed = name.trim();
        if (!trimmed) return;
        createToken.mutate(trimmed, {
            onSuccess: (created) => {
                setRawToken(created.raw_token);
                toast.success('Integration Token 已创建');
            },
            onError: (error) => showError('创建失败', error),
        });
    };

    const handleRevoke = (id: number, tokenName: string) => {
        if (!window.confirm(`确认撤销 ${tokenName}？撤销后 Upstream Hub 会立即失去访问权限。`)) return;
        revokeToken.mutate(id, {
            onSuccess: () => toast.success('Integration Token 已撤销'),
            onError: (error) => showError('撤销失败', error),
        });
    };

    const handleRotate = (id: number, tokenName: string) => {
        if (!window.confirm(`确认轮换 ${tokenName}？旧 Token 会立即失效，需要把新 Token 更新到 Upstream Hub。`)) return;
        rotateToken.mutate(id, {
            onSuccess: (created) => {
                setRawToken(created.raw_token);
                toast.success('Integration Token 已轮换');
            },
            onError: (error) => showError('轮换失败', error),
        });
    };

    const busy = createToken.isPending || revokeToken.isPending || rotateToken.isPending;

    return (
        <section className="rounded-3xl border border-border bg-card p-6 space-y-5">
            <div className="flex items-start justify-between gap-3">
                <div>
                    <h2 className="flex items-center gap-2 text-lg font-bold text-card-foreground">
                        <ShieldCheck className="size-5" />
                        Upstream Integration Token
                    </h2>
                    <p className="mt-1 text-xs leading-5 text-muted-foreground">
                        用于 Upstream Hub 的长期专用身份。默认不会过期，直到撤销或轮换；不能访问普通管理员 API。
                    </p>
                </div>
                <KeyRound className="size-5 text-muted-foreground" />
            </div>

            <div className="flex gap-2">
                <Input
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    placeholder="Token 名称"
                    className="h-9 rounded-xl"
                    disabled={busy}
                />
                <button
                    type="button"
                    onClick={handleCreate}
                    disabled={busy || !name.trim()}
                    className="flex h-9 shrink-0 items-center gap-1.5 rounded-xl bg-primary px-3 text-sm font-medium text-primary-foreground disabled:opacity-50"
                >
                    {createToken.isPending ? <Loader className="size-4 animate-spin" /> : <Plus className="size-4" />}
                    创建
                </button>
            </div>

            <div className="space-y-2">
                {isLoading ? (
                    <div className="flex h-24 items-center justify-center text-sm text-muted-foreground">
                        <Loader className="size-4 animate-spin" />
                    </div>
                ) : isError ? (
                    <div className="flex h-24 items-center justify-center text-sm text-destructive">加载失败</div>
                ) : tokens.length === 0 ? (
                    <div className="flex h-24 items-center justify-center text-sm text-muted-foreground">尚未创建专用 Token</div>
                ) : tokens.map((token) => (
                    <div key={token.id} className="rounded-2xl border border-border bg-muted/15 p-3">
                        <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0 space-y-1">
                                <div className="flex flex-wrap items-center gap-2">
                                    <span className="truncate text-sm font-medium">{token.name}</span>
                                    <Badge variant={token.revoked_at ? 'secondary' : 'default'}>
                                        {token.revoked_at ? '已撤销' : '有效'}
                                    </Badge>
                                </div>
                                <p className="font-mono text-xs text-muted-foreground">{token.token_hint}</p>
                                <p className="text-[11px] text-muted-foreground">
                                    创建：{new Date(token.created_at).toLocaleString()} · 最后使用：{token.last_used_at ? new Date(token.last_used_at).toLocaleString() : '从未'}
                                </p>
                                <div className="flex flex-wrap gap-1 pt-1">
                                    {token.scopes.map((scope) => <Badge key={scope} variant="outline">{scope}</Badge>)}
                                </div>
                            </div>
                            {!token.revoked_at ? (
                                <div className="flex shrink-0 gap-1">
                                    <button
                                        type="button"
                                        title="轮换"
                                        disabled={busy}
                                        onClick={() => handleRotate(token.id, token.name)}
                                        className="flex size-8 items-center justify-center rounded-lg text-muted-foreground hover:bg-muted disabled:opacity-50"
                                    >
                                        <RefreshCw className="size-4" />
                                    </button>
                                    <button
                                        type="button"
                                        title="撤销"
                                        disabled={busy}
                                        onClick={() => handleRevoke(token.id, token.name)}
                                        className="flex size-8 items-center justify-center rounded-lg text-destructive hover:bg-destructive/10 disabled:opacity-50"
                                    >
                                        <Trash2 className="size-4" />
                                    </button>
                                </div>
                            ) : null}
                        </div>
                    </div>
                ))}
            </div>

            <Dialog open={rawToken !== ''} onOpenChange={(open) => { if (!open) setRawToken(''); }}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>请立即保存 Token</DialogTitle>
                        <DialogDescription>
                            原文只显示这一次。关闭后 Octopus 无法再次读取；请保存到 Upstream Hub 的受限配置中。
                        </DialogDescription>
                    </DialogHeader>
                    <div className="flex items-center gap-2 rounded-xl border border-border bg-muted/30 p-3">
                        <code className="min-w-0 flex-1 break-all text-xs">{rawToken}</code>
                        <CopyIconButton
                            text={rawToken}
                            className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-background"
                            copyIconClassName="size-4"
                            checkIconClassName="size-4"
                        />
                    </div>
                    <DialogFooter>
                        <button
                            type="button"
                            onClick={() => setRawToken('')}
                            className="h-9 rounded-xl bg-primary px-4 text-sm font-medium text-primary-foreground"
                        >
                            我已保存，关闭
                        </button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </section>
    );
}
