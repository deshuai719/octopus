import { describe, expect, it } from 'vitest';
import type { SiteChannelGroup, SiteKeyCreateCapability } from '@/api/endpoints/site-channel';
import {
    buildKeyCreationView,
    summarizeSiteKeyCreateBatchResult,
    summarizeSiteKeyCreateResult,
} from './key-creation-view';

function group(groupKey: string, hasKeys: boolean): SiteChannelGroup {
    return {
        group_key: groupKey,
        group_name: groupKey.toUpperCase(),
        projection_disabled: false,
        projection_suspended: false,
        model_sync_status: 'idle',
        model_sync_authoritative: false,
        model_sync_model_count: 0,
        model_sync_failure_count: 0,
        key_count: hasKeys ? 1 : 0,
        enabled_key_count: hasKeys ? 1 : 0,
        masked_pending_key_count: 0,
        has_keys: hasKeys,
        has_projected_channel: false,
        projected_channel_ids: [],
        projected_channels: [],
        source_keys: [],
        projected_keys: [],
        models: [],
    };
}

function capability(overrides: Partial<SiteKeyCreateCapability> = {}): SiteKeyCreateCapability {
    return {
        supported: true,
        writable: true,
        can_create_single: true,
        can_create_all: true,
        ...overrides,
    };
}

describe('buildKeyCreationView', () => {
    it('shows single and batch creation only for writable missing groups', () => {
        const view = buildKeyCreationView([group('existing', true), group('missing', false)], capability());
        expect(view.pendingGroups.map((item) => item.group_key)).toEqual(['missing']);
        expect(view.canCreateSingle).toBe(true);
        expect(view.canCreateAll).toBe(true);
    });

    it('keeps missing groups visible while exposing the backend reason', () => {
        const view = buildKeyCreationView([group('missing', false)], capability({
            writable: false,
            can_create_single: false,
            can_create_all: false,
            reason_code: 'credential_api_key_read_only',
            reason: '当前账号只有模型调用 API Key',
        }));
        expect(view.pendingGroups).toHaveLength(1);
        expect(view.canCreateSingle).toBe(false);
        expect(view.canCreateAll).toBe(false);
        expect(view.unavailableReason).toBe('当前账号只有模型调用 API Key');
    });
});

describe('key creation summaries', () => {
    it('warns against repeating an upstream-applied single create', () => {
        expect(summarizeSiteKeyCreateResult({
            status: 'remote_created_sync_failed',
            remote_applied: true,
            sync_pending: true,
            message: '',
        }, 'VIP')).toContain('不要重复创建');
    });

    it('reports partial batch failures by group without upstream response bodies', () => {
        expect(summarizeSiteKeyCreateBatchResult({
            attempted_count: 2,
            created_count: 1,
            already_exists_count: 0,
            failed_count: 1,
            sync_pending: false,
            failures: [{ group_key: 'free', group_name: 'Free', message: 'sanitized' }],
        })).toBe('批量创建完成：创建 1 个，已存在 0 个，失败 1 个（Free）');
    });
});
