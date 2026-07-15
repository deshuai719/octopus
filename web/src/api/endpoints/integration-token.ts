import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';

export const UPSTREAM_INTEGRATION_SCOPES = [
    'upstream.import.read',
    'upstream.import.resolve',
    'upstream.auth.lease',
    'upstream.ratio.write',
] as const;

export interface IntegrationTokenMetadata {
    id: number;
    name: string;
    token_hint: string;
    scopes: string[];
    last_used_at?: string;
    revoked_at?: string;
    created_at: string;
    updated_at: string;
}

export interface IntegrationTokenCreated {
    token: IntegrationTokenMetadata;
    raw_token: string;
}

export function useIntegrationTokenList() {
    return useQuery({
        queryKey: ['integration-tokens', 'list'],
        queryFn: () => apiClient.get<IntegrationTokenMetadata[]>('/api/v1/integrations/tokens'),
    });
}

export function useCreateIntegrationToken() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (name: string) => apiClient.post<IntegrationTokenCreated>('/api/v1/integrations/tokens', {
            name,
            scopes: [...UPSTREAM_INTEGRATION_SCOPES],
        }),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['integration-tokens', 'list'] }),
    });
}

export function useRevokeIntegrationToken() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: number) => apiClient.post<null>(`/api/v1/integrations/tokens/${id}/revoke`),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['integration-tokens', 'list'] }),
    });
}

export function useRotateIntegrationToken() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: number) => apiClient.post<IntegrationTokenCreated>(`/api/v1/integrations/tokens/${id}/rotate`),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['integration-tokens', 'list'] }),
    });
}
