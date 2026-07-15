import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  runWithRoutedPagePermission,
  runWithTargetPermission,
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

  it("leaves successful routed-page permission lifecycle to the worker", async () => {
    const removePermission = vi.fn(async () => true);
    chrome.permissions.remove = removePermission;

    const response = await runWithRoutedPagePermission(
      "https://octopus.example.com",
      async () => ({ ok: true, mode: "binding" }),
    );

    expect(response).toEqual({ ok: true, mode: "binding" });
    expect(removePermission).not.toHaveBeenCalled();
  });

  it("revokes routed-page permission when worker communication fails", async () => {
    const removePermission = vi.fn(async () => true);
    chrome.permissions.remove = removePermission;
    const failure = new Error("worker unavailable");

    await expect(runWithRoutedPagePermission(
      "https://site.example.com",
      async () => { throw failure; },
    )).rejects.toBe(failure);

    expect(removePermission).toHaveBeenCalledWith({ origins: ["https://site.example.com/*"] });
  });

});
