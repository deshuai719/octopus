import { describe, expect, it } from "vitest";
import {
  readingView,
  submittingView,
  waitingView,
} from "../src/panel-state";

describe("side panel primary action", () => {
  it("offers direct page routing without recovery-session wording", () => {
    expect(waitingView("请读取当前页面")).toEqual({
      status: "准备连接 Octopus",
      detail: "请读取当前页面",
      primaryAction: "capture",
      primaryLabel: "读取当前标签页",
      primaryDisabled: false,
    });
    expect(waitingView().detail).toContain("Octopus 管理员页面");
    expect(readingView()).toMatchObject({
      status: "正在读取当前页面",
      detail: "正在识别 Octopus 或中转站登录状态。",
    });
  });

  it("makes the NewAPI token rotation action explicit", () => {
    expect(submittingView(true)).toMatchObject({
      primaryLabel: "正在生成/读取并验证",
      detail: expect.stringContaining("本次直接捕获最多自动生成一次"),
    });
  });
});
