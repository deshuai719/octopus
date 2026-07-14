import { describe, expect, it } from "vitest";
import { recoveryCandidateFailureMessage } from "../src/errors";

describe("recovery candidate errors", () => {
  it("translates known structured recovery codes", () => {
    expect(recoveryCandidateFailureMessage("site.recovery.expired", 410)).toBe("恢复会话已经过期");
  });

  it("does not surface an unknown server response body", () => {
    expect(recoveryCandidateFailureMessage("site.upstream.untrusted-detail", 502)).toBe("Octopus 验证失败（HTTP 502）");
  });
});
