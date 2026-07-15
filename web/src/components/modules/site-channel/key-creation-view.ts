import type {
    SiteChannelGroup,
    SiteKeyCreateBatchResult,
    SiteKeyCreateCapability,
    SiteKeyCreateResult,
} from '@/api/endpoints/site-channel';

export function buildKeyCreationView(
    groups: SiteChannelGroup[],
    capability: SiteKeyCreateCapability,
) {
    const pendingGroups = groups.filter((group) => !group.has_keys);
    return {
        pendingGroups,
        canCreateSingle: capability.can_create_single && capability.writable,
        canCreateAll: capability.can_create_all && capability.writable,
        unavailableReason: capability.reason || '当前账号不能调用上游分组 Key 创建接口',
    };
}

export function summarizeSiteKeyCreateResult(result: SiteKeyCreateResult, groupName: string) {
    switch (result.status) {
        case 'already_exists':
            return `分组「${groupName}」已存在 Key，未重复创建`;
        case 'remote_created_sync_failed':
            return `分组「${groupName}」已在上游创建，但本地同步失败；请先重新同步，不要重复创建`;
        default:
            return `分组「${groupName}」已创建 Key 并完成同步`;
    }
}

export function summarizeSiteKeyCreateBatchResult(result: SiteKeyCreateBatchResult) {
    const base = `批量创建完成：创建 ${result.created_count} 个，已存在 ${result.already_exists_count} 个，失败 ${result.failed_count} 个`;
    const failedGroups = result.failures
        .map((failure) => failure.group_name || failure.group_key)
        .filter(Boolean);
    const failureSuffix = failedGroups.length > 0 ? `（${failedGroups.join('、')}）` : '';
    const syncSuffix = result.sync_pending ? '；上游已有变更尚未同步，请先重新同步' : '';
    return `${base}${failureSuffix}${syncSuffix}`;
}
