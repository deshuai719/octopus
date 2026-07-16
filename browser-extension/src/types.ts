export type Platform =
  | "new-api"
  | "one-api"
  | "one-hub"
  | "done-hub"
  | "sub2api"
  | "anyrouter";

export type CandidateCredential = {
  access_token?: string;
  refresh_token?: string;
  token_expires_at?: number;
  platform_user_id?: number;
  identity_label?: string;
};

export type ExtractResult =
  | {
      kind: "candidate";
      candidate: CandidateCredential;
      generated_system_token?: boolean;
    }
  | {
      kind: "manual_required";
      message: string;
      reason?: "system_token_missing";
      suggested_paths?: string[];
      platform_user_id?: number;
      identity_label?: string;
    }
  | { kind: "not_logged_in"; message: string }
  | { kind: "error"; message: string };

export type WorkerRequest =
  | { type: "capture_active_session"; expected_origin: string }
  | { type: "get_active_context"; origin?: string }
  | { type: "confirm_binding_replacement" }
  | { type: "generate_direct_token"; origin: string }
  | { type: "submit_direct_token"; origin: string; access_token: string }
  | { type: "confirm_direct_capture"; origin: string; capture_id: string; preview_version: string; site_name?: string; account_name?: string; add_tags?: string[] }
  | { type: "resolve_direct_capture"; origin: string; capture_id: string; account_id?: number; create_new?: boolean }
  | { type: "cancel_direct_capture"; origin: string; capture_id: string }
  | { type: "retry_direct_sync"; origin: string; capture_id: string }
  | { type: "clear_direct_session"; origin: string }
  | { type: "get_direct_capture_summary"; refresh_active?: boolean }
  | { type: "open_direct_capture_origin"; origin: string }
  | { type: "clear_diagnostics" };

export type WorkerResponse = {
  ok: boolean;
  result?: ExtractResult;
  binding?: Omit<OctopusBinding, "token">;
  capture?: DirectCaptureView;
  summary?: DirectCaptureSummaryItem[];
  origin?: string;
  mode?: "binding" | "direct" | "unsupported";
  message?: string;
};

export type SessionEvent =
  | { type: "operation_error"; message: string }
  | { type: "binding_updated"; origin: string }
  | { type: "binding_replacement_required"; current_origin: string; next_origin: string }
  | { type: "direct_capture_progress"; origin: string; phase: "previewing" }
  | { type: "direct_capture_updated"; origin: string; capture: DirectCaptureView }
  | { type: "direct_capture_manual_required"; origin: string; operation_id: string; platform: Platform; reason: string; message: string };

export type OctopusBinding = {
  version: 1;
  origin: string;
  token: string;
  expire_at: string;
  validated_at: string;
};

export type PlatformEvidence = { code: string; value?: string };

export type DirectCaptureCandidate = {
  origin: string;
  platform: Platform;
  access_token?: string;
  refresh_token?: string;
  token_expires_at?: number;
  platform_user_id?: number;
  identity_label?: string;
  evidence: PlatformEvidence[];
};

export type DirectCaptureView = {
  capture_id: string;
  operation_id: string;
  origin: string;
  platform: Platform;
  phase: string;
  expires_at: string;
  preview_version?: string;
  action?: string;
  site_id?: number;
  site_name?: string;
  site_archived?: boolean;
  site_enabled?: boolean;
  account_id?: number;
  account_name?: string;
  account_enabled?: boolean;
  site_tags?: string[];
  credential_migration?: boolean;
  account_options?: Array<{
    id: number;
    name: string;
    credential_type: string;
    platform_user_id?: number;
    enabled: boolean;
  }>;
  candidate?: {
    credential_type: string;
    access_token_mask: string;
    has_refresh_token: boolean;
    token_expires_at?: number;
    platform_user_id?: number;
    identity_label?: string;
    evidence_codes?: string[];
  };
  saved?: { action: string; site_id: number; account_id: number };
  sync_result?: { status: string; message: string };
  error_code?: string;
  error_message?: string;
  page_title?: string;
  tag_update_supported?: boolean;
};

export type DirectCaptureSummaryItem = {
  origin: string;
  capture_id?: string;
  site_name?: string;
  phase: string;
  saved: boolean;
  created_at: string;
  updated_at: string;
  sync_started_at?: string;
  expires_at: string;
  can_retry_sync: boolean;
  can_clear: boolean;
  error_code?: string;
  error_message?: string;
  sync_result?: { status: string; message: string };
};
