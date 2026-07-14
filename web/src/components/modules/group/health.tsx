'use client';

import { useMemo, useState } from 'react';
import { Activity, ChevronDown, Clock3, Copy, LoaderCircle, Play, Trash2 } from 'lucide-react';
import { useLocale, useTranslations } from 'next-intl';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
    DialogTrigger,
} from '@/components/ui/dialog';
import { cn } from '@/lib/utils';
import { useGroupHealthEnabled } from '@/api/endpoints/setting';
import { useUpdateGroup } from '@/api/endpoints/group';
import { toast } from '@/components/common/Toast';
import {
    useGroupHealthList,
    useRunGroupHealth,
    type GroupHealthAttempt,
    type GroupHealthAttemptStatus,
    type GroupHealthProbeMode,
    type GroupHealthStatus,
} from '@/api/endpoints/group-health';

function formatDateTime(value?: string | null) {
    if (!value) return 'Never';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return 'Never';
    return date.toLocaleString();
}

function formatRelativeTime(value: string | null | undefined, locale: string, fallback: string) {
    if (!value) return fallback;
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return fallback;

    const diffSeconds = Math.round((date.getTime() - Date.now()) / 1000);
    const absSeconds = Math.abs(diffSeconds);
    const formatter = new Intl.RelativeTimeFormat(locale, { numeric: 'always' });

    if (absSeconds < 60) return formatter.format(diffSeconds, 'second');
    const diffMinutes = Math.round(diffSeconds / 60);
    if (Math.abs(diffMinutes) < 60) return formatter.format(diffMinutes, 'minute');
    const diffHours = Math.round(diffMinutes / 60);
    if (Math.abs(diffHours) < 24) return formatter.format(diffHours, 'hour');
    const diffDays = Math.round(diffHours / 24);
    return formatter.format(diffDays, 'day');
}

function statusLabel(status?: GroupHealthStatus | null) {
    return status ?? 'idle';
}

function statusDotTone(status?: GroupHealthStatus | null) {
    switch (status) {
        case 'success':
            return 'bg-emerald-500';
        case 'partial':
            return 'bg-amber-500';
        case 'running':
            return 'bg-sky-500 animate-pulse';
        case 'failed':
            return 'bg-destructive';
        default:
            return 'bg-muted-foreground/40';
    }
}

function statusTextTone(status?: GroupHealthStatus | null) {
    switch (status) {
        case 'success':
            return 'text-emerald-600 dark:text-emerald-400';
        case 'partial':
            return 'text-amber-600 dark:text-amber-400';
        case 'running':
            return 'text-sky-600 dark:text-sky-400';
        case 'failed':
            return 'text-destructive';
        default:
            return 'text-muted-foreground';
    }
}

function probeModeTone(mode?: GroupHealthProbeMode | null) {
    return mode === 'full'
        ? 'border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300'
        : 'border-border bg-muted/40 text-muted-foreground';
}

function attemptBadgeTone(status: GroupHealthAttemptStatus) {
    switch (status) {
        case 'success':
            return 'border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300';
        case 'skipped':
            return 'border-border bg-muted/40 text-muted-foreground';
        case 'failed':
        default:
            return 'border-destructive/20 bg-destructive/10 text-destructive';
    }
}

function safeDiagnosticMessage(attempt: GroupHealthAttempt) {
    const error = attempt.error_message.toLowerCase();
    if (attempt.http_status > 0) return `上游返回 HTTP ${attempt.http_status}`;
    if (error.includes('deadline') || error.includes('timeout') || error.includes('timed out')) return '探测请求超时';
    if (error.includes('no available key')) return '渠道没有可用 Key';
    if (error.includes('failed to load channel')) return '无法加载渠道配置';
    return attempt.status === 'failed' ? '探测失败；原始错误正文已从诊断上下文省略' : '探测完成';
}

function suggestedDiagnosticAction(attempt: GroupHealthAttempt) {
    if (attempt.http_status === 401 || attempt.http_status === 403) return '检查上游 Key 权限、账号登录状态和模型授权';
    if (attempt.http_status === 404) return '检查模型名、上游 API 路径和协议类型';
    if (attempt.http_status === 429) return '检查额度、并发限制和上游限流策略';
    if (attempt.http_status >= 500) return '稍后重试，并检查上游站点状态';
    if (attempt.probe_profile === 'public_compat' && attempt.duration_ms >= 45000) return '公益站已按 45 秒边界等待；检查排队情况或稍后重试';
    return '检查渠道端点、模型映射和网络连通性';
}

export function buildGroupHealthDiagnosticContext(attempt: GroupHealthAttempt) {
    return JSON.stringify({
        schema: 'octopus.group_health_diagnostic.v1',
        status: attempt.status,
        error_code: attempt.http_status > 0 ? `http_${attempt.http_status}` : 'probe_failed',
        error_message: safeDiagnosticMessage(attempt),
        http_status: attempt.http_status || null,
        duration_ms: attempt.duration_ms,
        timeout_ms: attempt.probe_profile === 'public_compat' ? 45000 : 12000,
        model: attempt.model_name,
        channel: {
            id: attempt.channel_id,
            name: attempt.channel_name,
        },
        site: attempt.site_id > 0 ? {
            id: attempt.site_id,
            name: attempt.site_name,
            tags: attempt.site_tags,
            group_name: attempt.site_group_name,
            group_ratio: attempt.site_group_ratio ?? null,
            group_ratio_seen_at: attempt.site_group_ratio_seen_at ?? null,
        } : null,
        probe_profile: attempt.probe_profile,
        suggested_action: suggestedDiagnosticAction(attempt),
        redaction: 'credential values, request bodies, response bodies, cookies and authorization headers are excluded',
    }, null, 2);
}

export function GroupHealthAttemptDetails({
    attempt,
    selected = false,
    onSelectionChange,
    onDelete,
    deleting = false,
}: {
    attempt: GroupHealthAttempt;
    selected?: boolean;
    onSelectionChange?: (selected: boolean) => void;
    onDelete?: () => void;
    deleting?: boolean;
}) {
    const t = useTranslations('group.health');
    const hasError = Boolean(attempt.error_message);
    const selectable = attempt.status === 'failed' && attempt.group_item_id > 0 && Boolean(onSelectionChange);
    const canDelete = attempt.status === 'failed' && attempt.group_item_id > 0 && Boolean(onDelete);

    const handleCopyDiagnostic = async (event: React.MouseEvent<HTMLButtonElement>) => {
        event.preventDefault();
        event.stopPropagation();
        try {
            await navigator.clipboard.writeText(buildGroupHealthDiagnosticContext(attempt));
            toast.success('已复制脱敏 AI 诊断上下文');
        } catch {
            toast.error('复制诊断上下文失败');
        }
    };

    const content = (
        <div className="grid grid-cols-[auto_1rem_minmax(0,1fr)_auto] items-start gap-x-2 text-xs">
            <div className="flex h-5 min-w-4 items-center justify-center">
                {selectable ? (
                    <input
                        type="checkbox"
                        checked={selected}
                        onClick={(event) => event.stopPropagation()}
                        onChange={(event) => onSelectionChange?.(event.target.checked)}
                        className="size-4 rounded border-border bg-background accent-primary"
                        aria-label={`选择失败项 ${attempt.channel_name}`}
                    />
                ) : null}
            </div>
            <div className="flex h-5 items-center justify-center text-muted-foreground">
                {hasError ? <ChevronDown className="size-3.5 transition-transform group-open:rotate-180" /> : null}
            </div>
            <div className="min-w-0">
                <div className="truncate font-medium leading-5">
                    {attempt.channel_name}
                    {attempt.key_remark ? ` / ${attempt.key_remark}` : ''}
                </div>
                <div className="mt-1 flex min-w-0 items-center gap-2 overflow-hidden whitespace-nowrap leading-4 text-muted-foreground">
                    <span className="shrink-0">{attempt.http_status ? `HTTP ${attempt.http_status}` : t('noHttpStatus')}</span>
                    <span className="shrink-0">·</span>
                    <span className="shrink-0">{attempt.duration_ms}ms</span>
                    {attempt.model_name ? <><span className="shrink-0">·</span><span className="min-w-0 truncate">{attempt.model_name}</span></> : null}
                </div>
                <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[11px] leading-4 text-muted-foreground">
                    {attempt.site_name ? <span>{attempt.site_name}</span> : <span>非托管渠道</span>}
                    {attempt.site_tags.map((tag) => (
                        <Badge key={tag} variant="outline" className="h-4 px-1 text-[9px]">{tag}</Badge>
                    ))}
                    {attempt.site_group_name ? <span>· {attempt.site_group_name}</span> : null}
                    {attempt.site_tags.includes('付费') ? (
                        <span>
                            · 倍率 {attempt.site_group_ratio == null ? '未同步' : `${attempt.site_group_ratio}x`}
                            {attempt.site_group_ratio_seen_at ? `（${formatDateTime(attempt.site_group_ratio_seen_at)}）` : ''}
                        </span>
                    ) : null}
                    <Badge variant="outline" className={cn(
                        'h-4 px-1 text-[9px]',
                        attempt.probe_profile === 'public_compat'
                            ? 'border-sky-500/25 bg-sky-500/10 text-sky-700 dark:text-sky-300'
                            : 'border-border text-muted-foreground',
                    )}>
                        {attempt.probe_profile === 'public_compat' ? '公益兼容 · 最长 45 秒' : '标准 · 最长 12 秒'}
                    </Badge>
                </div>
            </div>
            <div className="flex shrink-0 items-center gap-1">
                <Badge variant="outline" className={cn('shrink-0 text-[11px]', attemptBadgeTone(attempt.status))}>
                    {t(`attemptStatus.${attempt.status}`)}
                </Badge>
                <Button type="button" variant="ghost" size="icon" className="size-7 rounded-lg" onClick={handleCopyDiagnostic} title="复制脱敏 AI 诊断上下文">
                    <Copy className="size-3.5" />
                </Button>
                {canDelete ? (
                    <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        className="size-7 rounded-lg text-destructive hover:bg-destructive/10 hover:text-destructive"
                        disabled={deleting}
                        onClick={(event) => {
                            event.preventDefault();
                            event.stopPropagation();
                            onDelete?.();
                        }}
                        title="从当前 Octopus 分组删除"
                    >
                        {deleting ? <LoaderCircle className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}
                    </Button>
                ) : null}
            </div>
        </div>
    );

    if (!hasError) {
        return (
            <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs transition-[border-color,box-shadow] hover:border-border hover:shadow-sm">
                <CardContent className="px-3 py-2 text-xs">
                    {content}
                </CardContent>
            </Card>
        );
    }

    return (
        <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs transition-[border-color,box-shadow] hover:border-border hover:shadow-sm">
            <details className="group">
                <summary className="cursor-pointer list-none px-3 py-2 text-xs [&::-webkit-details-marker]:hidden">
                    {content}
                </summary>
                <div className="mx-3 mb-2 ml-9 max-h-36 overflow-y-auto whitespace-pre-wrap break-all border-t border-border/60 pt-2 text-xs leading-relaxed text-muted-foreground">
                    <div className="mb-1 font-medium text-foreground">{t('errorDetails')}</div>
                    {attempt.error_message}
                </div>
            </details>
        </Card>
    );
}

export function GroupHealthBadge({ groupId }: { groupId?: number }) {
    const t = useTranslations('group.health');
    const locale = useLocale();
    const { enabled } = useGroupHealthEnabled();
    const { data: views = [] } = useGroupHealthList();
    const runGroupHealth = useRunGroupHealth();
    const updateGroup = useUpdateGroup();
    const [open, setOpen] = useState(false);
    const [selectedFailedItemIds, setSelectedFailedItemIds] = useState<Set<number>>(new Set());

    const view = useMemo(
        () => views.find((item) => item.group_id === groupId),
        [groupId, views]
    );
    const latest = view?.latest ?? null;
    const attempts = useMemo(() => latest?.attempts ?? [], [latest?.attempts]);
    const successCount = attempts.filter((attempt) => attempt.status === 'success').length;
    const failedItemIds = useMemo(
        () => new Set(attempts.filter((attempt) => attempt.status === 'failed' && attempt.group_item_id > 0).map((attempt) => attempt.group_item_id)),
        [attempts],
    );
    const selectedFailureIds = useMemo(
        () => Array.from(selectedFailedItemIds).filter((id) => failedItemIds.has(id)),
        [failedItemIds, selectedFailedItemIds],
    );

    if (!enabled || !groupId) return null;

    const isRunning = latest?.status === 'running';
    const isRunPendingForGroup = runGroupHealth.isPending
        && runGroupHealth.variables?.groupId === groupId;
    const isStandardRunPending = isRunPendingForGroup
        && (runGroupHealth.variables?.probeMode ?? 'standard') === 'standard';
    const isFullRunPending = isRunPendingForGroup
        && runGroupHealth.variables?.probeMode === 'full';
    const lastRunRelative = formatRelativeTime(latest?.finished_at ?? latest?.started_at ?? null, locale, t('never'));

    const toggleFailedItem = (itemId: number, selected: boolean) => {
        setSelectedFailedItemIds((current) => {
            const next = new Set(current);
            if (selected) next.add(itemId);
            else next.delete(itemId);
            return next;
        });
    };

    const deleteFailedItems = async (itemIds: number[]) => {
        const uniqueIds = Array.from(new Set(itemIds.filter((id) => id > 0 && failedItemIds.has(id))));
        if (uniqueIds.length === 0) return;
        if (!window.confirm(`确认从当前 Octopus 分组删除 ${uniqueIds.length} 个测活失败项？不会删除上游 Key、站点、账号或渠道，历史测活记录会保留。`)) {
            return;
        }
        try {
            await updateGroup.mutateAsync({ id: groupId, items_to_delete: uniqueIds });
            setSelectedFailedItemIds((current) => {
                const next = new Set(current);
                uniqueIds.forEach((id) => next.delete(id));
                return next;
            });
            toast.success(`已从当前分组删除 ${uniqueIds.length} 个失败项`);
        } catch {
            toast.error('删除失败项失败；结果可能已经变化，请刷新后重试');
        }
    };

    const deletingItemIds = new Set(updateGroup.isPending ? updateGroup.variables?.items_to_delete ?? [] : []);

    return (
        <Dialog open={open} onOpenChange={setOpen}>
            <Card className="mb-3 gap-0 rounded-xl border-border/70 bg-background/80 py-0 shadow-none">
                <CardContent className="flex items-center justify-between gap-2 px-3 py-1.5">
                    <DialogTrigger asChild>
                        <button type="button" className="grid min-w-0 flex-1 grid-cols-[auto_auto_minmax(0,1fr)] items-center gap-x-2 gap-y-0.5 text-left">
                            <span className={cn('row-span-2 size-2 rounded-full self-center', statusDotTone(latest?.status))} />
                            <span className="text-sm font-medium leading-5 text-foreground">{t('title')}</span>
                            <span className="min-w-0 truncate text-xs leading-5 text-muted-foreground">
                                {lastRunRelative}
                            </span>
                            <span className="col-start-2 col-span-2 flex min-w-0 items-center gap-3 text-xs leading-4 text-muted-foreground">
                                <Badge variant="outline" className={cn('h-5 px-1.5 text-[10px] uppercase tracking-wide', probeModeTone(latest?.probe_mode ?? 'standard'))}>
                                    {t(`probeMode.${latest?.probe_mode ?? 'standard'}`)}
                                </Badge>
                                <span className="inline-flex items-center gap-1">
                                    <Activity className="size-3.5" />
                                    {successCount}/{attempts.length || 0}
                                </span>
                                <span className="inline-flex items-center gap-1">
                                    <Clock3 className="size-3.5" />
                                    {latest?.duration_ms ?? 0}ms
                                </span>
                            </span>
                        </button>
                    </DialogTrigger>
                    <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        className="h-7 rounded-lg px-2 text-xs"
                        disabled={isRunPendingForGroup || isRunning}
                        onClick={() => runGroupHealth.mutate({ groupId })}
                    >
                        {isRunning || isStandardRunPending ? <LoaderCircle className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}
                        {t('run')}
                    </Button>
                    <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        className="h-7 rounded-lg px-2 text-xs"
                        disabled={isRunPendingForGroup || isRunning}
                        onClick={() => runGroupHealth.mutate({ groupId, probeMode: 'full' })}
                    >
                        {isFullRunPending ? <LoaderCircle className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}
                        {t('runFull')}
                    </Button>
                </CardContent>
            </Card>

            <DialogContent className="flex h-[min(85vh,42rem)] flex-col overflow-hidden rounded-3xl sm:max-w-2xl">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        <span className={cn('size-2.5 rounded-full', statusDotTone(latest?.status))} />
                        {t('detailTitle')}
                    </DialogTitle>
                    <DialogDescription>
                        {t('lastRun', { time: formatDateTime(latest?.finished_at ?? latest?.started_at ?? null) })}
                    </DialogDescription>
                </DialogHeader>

                <div className="grid grid-cols-2 gap-2 text-sm md:grid-cols-4">
                    <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs">
                        <CardContent className="p-3">
                            <div className="text-xs text-muted-foreground">{t('status')}</div>
                            <div className={cn('mt-1 font-medium', statusTextTone(latest?.status))}>{t(`statusValue.${statusLabel(latest?.status)}`)}</div>
                            <Badge variant="outline" className={cn('mt-2 h-5 px-1.5 text-[10px] uppercase tracking-wide', probeModeTone(latest?.probe_mode ?? 'standard'))}>
                                {t(`probeMode.${latest?.probe_mode ?? 'standard'}`)}
                            </Badge>
                        </CardContent>
                    </Card>
                    <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs">
                        <CardContent className="p-3">
                            <div className="text-xs text-muted-foreground">{t('healthy')}</div>
                            <div className="mt-1 font-medium">{successCount}/{attempts.length || 0}</div>
                        </CardContent>
                    </Card>
                    <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs">
                        <CardContent className="p-3">
                            <div className="text-xs text-muted-foreground">{t('duration')}</div>
                            <div className="mt-1 font-medium">{latest?.duration_ms ?? 0}ms</div>
                        </CardContent>
                    </Card>
                    <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs">
                        <CardContent className="p-3">
                            <div className="text-xs text-muted-foreground">{t('attempts')}</div>
                            <div className="mt-1 font-medium">{attempts.length}</div>
                        </CardContent>
                    </Card>
                </div>

                {failedItemIds.size > 0 ? (
                    <div className="flex flex-wrap items-center justify-between gap-2 rounded-2xl border border-destructive/20 bg-destructive/5 px-3 py-2">
                        <div className="text-xs text-muted-foreground">
                            可直接清理失败成员；只修改当前 Octopus 分组，历史结果仍保留。
                        </div>
                        <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            className="h-8 rounded-xl border-destructive/30 text-destructive hover:bg-destructive/10 hover:text-destructive"
                            disabled={selectedFailureIds.length === 0 || updateGroup.isPending}
                            onClick={() => deleteFailedItems(selectedFailureIds)}
                        >
                            {updateGroup.isPending ? <LoaderCircle className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}
                            删除所选失败项（{selectedFailureIds.length}）
                        </Button>
                    </div>
                ) : null}

                <div className="min-h-0 flex-1 space-y-2 overflow-y-auto pr-1">
                    {attempts.length ? attempts.map((attempt) => (
                        <GroupHealthAttemptDetails
                            key={attempt.id}
                            attempt={attempt}
                            selected={selectedFailedItemIds.has(attempt.group_item_id)}
                            onSelectionChange={attempt.status === 'failed' && attempt.group_item_id > 0
                                ? (selected) => toggleFailedItem(attempt.group_item_id, selected)
                                : undefined}
                            onDelete={attempt.status === 'failed' && attempt.group_item_id > 0
                                ? () => deleteFailedItems([attempt.group_item_id])
                                : undefined}
                            deleting={deletingItemIds.has(attempt.group_item_id)}
                        />
                    )) : (
                        <div className="rounded-2xl border border-dashed border-border/70 bg-muted/20 px-3 py-6 text-center text-xs text-muted-foreground">
                            {t('empty')}
                        </div>
                    )}
                </div>
            </DialogContent>
        </Dialog>
    );
}
