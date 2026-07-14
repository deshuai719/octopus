export type Platform =
  | "new-api"
  | "one-api"
  | "one-hub"
  | "done-hub"
  | "sub2api"
  | "anyrouter";

export type CredentialField =
  | "access_token"
  | "refresh_token"
  | "token_expires_at"
  | "platform_user_id";

export type AuthCapability = {
  compatible_family: "new-api" | "sub2api" | "anyrouter";
  required_fields: CredentialField[];
  extractable_fields: CredentialField[];
  recovery_guide: {
    title: string;
    steps: string[];
    manual_fallback: string;
  };
};

export type RecoveryPacket = {
  version: 1;
  api_base_url: string;
  session_id: string;
  capability: string;
  account_id: number;
  site_id: number;
  origin: string;
  platform: Platform;
  expires_at: string;
  auth: AuthCapability;
};

export type CandidateCredential = {
  account_id: number;
  origin: string;
  platform: Platform;
  access_token?: string;
  refresh_token?: string;
  token_expires_at?: number;
  platform_user_id?: number;
  identity_label?: string;
};

export type ExtractResult =
  | {
      kind: "candidate";
      candidate: Omit<CandidateCredential, "account_id" | "origin" | "platform">;
      generated_system_token?: boolean;
    }
  | {
      kind: "manual_required";
      message: string;
      reason?: "system_token_missing";
      suggested_paths?: string[];
    }
  | { kind: "not_logged_in"; message: string }
  | { kind: "error"; message: string };

export type WorkerRequest =
  | { type: "get_session" }
  | { type: "capture_active_session"; expected_origin: string }
  | { type: "open_target" }
  | { type: "extract_and_submit" }
  | { type: "discard_session" }
  | { type: "page_terminal"; session_id: string };

export type WorkerResponse = {
  ok: boolean;
  packet?: RecoveryPacket;
  result?: ExtractResult;
  message?: string;
};

export type SessionEvent =
  | { type: "session_updated"; packet: RecoveryPacket }
  | { type: "session_error"; message: string };
