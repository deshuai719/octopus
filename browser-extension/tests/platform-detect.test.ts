import { afterEach, describe, expect, it, vi } from "vitest";

import { detectCurrentPlatform, PLATFORM_STATUS_SIGNATURES } from "../src/platform-detect";

function installPage(storage: Record<string, string> = {}, title = "Custom Gateway") {
  vi.stubGlobal("location", { origin: "https://relay.example" });
  vi.stubGlobal("document", { title });
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => storage[key] ?? null,
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("detectCurrentPlatform", () => {
  it("uses a structural NewAPI status signature", async () => {
    installPage();
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({
      success: true,
      data: { quota_display_type: "USD", passkey_login: true, setup: true, system_name: "Renamed" },
    }), { status: 200 })));

    await expect(detectCurrentPlatform(PLATFORM_STATUS_SIGNATURES)).resolves.toEqual({
      platform: "new-api",
      evidence: [{ code: "browser.strong.status_schema.new-api" }],
      system_name: "Renamed",
    });
  });

  it("does not promote a title or system name to decisive evidence", async () => {
    installPage({}, "OneHub - My Relay");
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/status")) {
        return new Response(JSON.stringify({ success: true, data: { system_name: "OneHub" } }), { status: 200 });
      }
      return new Response("{}", { status: 401 });
    }));

    const result = await detectCurrentPlatform(PLATFORM_STATUS_SIGNATURES);
    expect(result.platform).toBeUndefined();
    expect(result.reason).toBe("variant_inconclusive");
    expect(result.evidence).toEqual([{ code: "browser.weak.brand.one-hub" }]);
  });

  it("requires both Sub2API storage and authenticated profile signals", async () => {
    installPage({ auth_token: "ephemeral-token", refresh_token: "ephemeral-refresh" });
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/api/v1/profile")) {
        return new Response(JSON.stringify({ data: { id: 7, email: "masked@example.invalid" } }), { status: 200 });
      }
      return new Response(JSON.stringify({ success: true, data: { version: "test" } }), { status: 200 });
    }));

    const result = await detectCurrentPlatform(PLATFORM_STATUS_SIGNATURES);
    expect(result.platform).toBe("sub2api");
    expect(result.evidence.map((item) => item.code)).toEqual([
      "browser.medium.storage.sub2api_token_pair",
      "browser.medium.auth_profile.sub2api",
    ]);
  });
});
