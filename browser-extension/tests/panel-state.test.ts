import { describe, expect, it } from "vitest";
import {
  candidateDeliveredView,
  loginRequiredView,
  readyView,
  submittingView,
  waitingView,
} from "../src/panel-state";

describe("side panel primary action", () => {
  it("offers one corrective action while no Octopus session is available", () => {
    expect(waitingView("当前页面没有恢复会话")).toEqual({
      status: "等待 Octopus 会话",
      detail: "当前页面没有恢复会话",
      primaryAction: "capture",
      primaryLabel: "读取当前标签页",
      primaryDisabled: false,
    });
  });

  it("advances through grant, login, submitting, and delivered states", () => {
    expect(readyView("本地 NewAPI 模拟恢复")).toMatchObject({
      status: "会话已就绪",
      primaryAction: "grant",
      primaryLabel: "允许访问并打开登录页",
      primaryDisabled: false,
    });
    expect(loginRequiredView()).toMatchObject({
      status: "等待完成登录",
      primaryAction: "extract",
      primaryLabel: "我已完成登录，提取并验证",
      primaryDisabled: false,
    });
    expect(submittingView()).toMatchObject({
      status: "正在提取并验证",
      primaryAction: null,
      primaryLabel: "正在提取并验证",
      primaryDisabled: true,
    });
    expect(candidateDeliveredView()).toMatchObject({
      status: "候选凭据已送达",
      primaryAction: null,
      primaryLabel: "候选凭据已送达",
      primaryDisabled: true,
    });
  });

  it("makes the NewAPI token rotation action explicit", () => {
    expect(loginRequiredView(true)).toMatchObject({
      primaryLabel: "生成/读取系统令牌并验证",
      detail: expect.stringContaining("可能更新旧系统令牌"),
    });
    expect(submittingView(true)).toMatchObject({
      primaryLabel: "正在生成/读取并验证",
    });
  });
});
