import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  grantPermissionAndOpenTarget,
  runWithTargetPermission,
  runWithTemporaryPagePermission,
} from "../src/permissions";

let requestPermission: ReturnType<typeof vi.fn<(permissions: chrome.permissions.Permissions) => Promise<boolean>>>;

describe("target origin permission", () => {
  beforeEach(() => {
    requestPermission = vi.fn(async () => true);
    vi.stubGlobal("chrome", {
      permissions: {
        request: requestPermission,
      },
    });
  });

  it("requests the exact origin permission before asking the worker to open the target", async () => {
    const calls: string[] = [];
    requestPermission.mockImplementation(async () => {
      calls.push("permission");
      return true;
    });
    const openTarget = vi.fn(async () => {
      calls.push("open");
      return { ok: true };
    });

    const response = await grantPermissionAndOpenTarget("http://127.0.0.1:4178", openTarget);

    expect(response).toEqual({ ok: true });
    expect(requestPermission).toHaveBeenCalledWith({ origins: ["http://127.0.0.1:4178/*"] });
    expect(calls).toEqual(["permission", "open"]);
  });

  it("does not open the target when the user rejects the permission", async () => {
    requestPermission.mockResolvedValue(false);
    const openTarget = vi.fn(async () => ({ ok: true }));

    const response = await grantPermissionAndOpenTarget("https://site.example.com", openTarget);

    expect(response).toEqual({ ok: false, message: "未获得目标站点临时权限" });
    expect(openTarget).not.toHaveBeenCalled();
  });

  it("rechecks the exact origin permission before extraction after target navigation", async () => {
    const calls: string[] = [];
    requestPermission.mockImplementation(async () => {
      calls.push("permission");
      return true;
    });
    const extract = vi.fn(async () => {
      calls.push("extract");
      return { ok: true };
    });

    const response = await runWithTargetPermission("https://site.example.com", extract);

    expect(response).toEqual({ ok: true });
    expect(requestPermission).toHaveBeenCalledWith({ origins: ["https://site.example.com/*"] });
    expect(calls).toEqual(["permission", "extract"]);
  });

  it("grants the Octopus page only for the duration of session capture", async () => {
    const calls: string[] = [];
    requestPermission.mockImplementation(async () => {
      calls.push("permission");
      return true;
    });
    const removePermission = vi.fn(async () => {
      calls.push("remove");
      return true;
    });
    chrome.permissions.remove = removePermission;
    const capture = vi.fn(async () => {
      calls.push("capture");
      return { ok: true };
    });

    const response = await runWithTemporaryPagePermission("https://octopus.example.com", capture);

    expect(response).toEqual({ ok: true });
    expect(requestPermission).toHaveBeenCalledWith({ origins: ["https://octopus.example.com/*"] });
    expect(removePermission).toHaveBeenCalledWith({ origins: ["https://octopus.example.com/*"] });
    expect(calls).toEqual(["permission", "capture", "remove"]);
  });
});
