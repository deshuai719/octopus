import { describe, expect, it } from "vitest";

import { captureReceiptFor, transferProgressForPhase } from "../src/capture-progress";
import type { DirectCaptureView } from "../src/types";

const states = (phase: string) => transferProgressForPhase(phase).map((step) => step.state);

const capture = (phase: string, overrides: Partial<DirectCaptureView> = {}): DirectCaptureView => ({
  capture_id: "capture-id",
  operation_id: "operation-id",
  origin: "https://relay.example",
  platform: "done-hub",
  phase,
  expires_at: "2099-01-01T00:00:00Z",
  action: "create_account",
  site_name: "示例站点",
  account_name: "管理账号",
  ...overrides,
});

describe("direct capture progress", () => {
  it("distinguishes preview delivery from account persistence", () => {
    expect(states("preview_ready")).toEqual(["complete", "complete", "pending", "pending"]);
    expect(captureReceiptFor(capture("preview_ready"))).toBeUndefined();
  });

  it("shows account persistence before synchronization completes", () => {
    expect(states("saved_syncing")).toEqual(["complete", "complete", "complete", "active"]);
    expect(captureReceiptFor(capture("saved_syncing", {
      saved: { action: "create_account", site_id: 65, account_id: 68 },
    }))).toEqual({
      action: "为已有站点新增账号",
      site: "示例站点 · ID 65",
      account: "管理账号 · ID 68",
      saved: "已写入 Octopus",
      synced: "正在同步",
      message: "账号和凭据已保存，正在后台同步站点数据。",
    });
  });

  it("keeps a saved receipt when synchronization fails", () => {
    expect(states("sync_failed")).toEqual(["complete", "complete", "complete", "failed"]);
    expect(captureReceiptFor(capture("sync_failed", {
      saved: { action: "create_account", site_id: 65, account_id: 68 },
      error_message: "同步暂时失败，可以重试。",
    }))).toMatchObject({
      saved: "已写入 Octopus",
      synced: "同步失败，可重试",
      message: "同步暂时失败，可以重试。",
    });
  });

  it("shows a complete, non-sensitive result receipt", () => {
    expect(states("completed")).toEqual(["complete", "complete", "complete", "complete"]);
    expect(captureReceiptFor(capture("completed", {
      saved: { action: "create_account", site_id: 65, account_id: 68 },
      sync_result: { status: "success", message: "已同步 7 个分组。" },
    }))).toEqual({
      action: "为已有站点新增账号",
      site: "示例站点 · ID 65",
      account: "管理账号 · ID 68",
      saved: "已写入 Octopus",
      synced: "同步成功",
      message: "已同步 7 个分组。",
    });
  });

  it("keeps preview delivery complete when confirmation conflicts or expires", () => {
    expect(states("conflict")).toEqual(["complete", "complete", "failed", "pending"]);
    expect(states("expired")).toEqual(["complete", "complete", "failed", "pending"]);
  });
});
