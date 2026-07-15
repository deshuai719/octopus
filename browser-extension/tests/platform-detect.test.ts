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

  it.each([
    ["done-hub", { linuxDo_oauth: true, user_agreement_enabled: true, max_log_query_days: 30 }],
    ["one-hub", { oidc_auth: true, language: "zh", EnableSafe: true, UptimeDomain: "" }],
    ["one-api", { oidc: true, oidc_well_known: "", oidc_token_endpoint: "" }],
  ] as const)("keeps the canonical %s status signature", async (platform, data) => {
    installPage();
    const paths: string[] = [];
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      paths.push(new URL(String(input)).pathname);
      return new Response(JSON.stringify({ success: true, data }), { status: 200 });
    }));

    await expect(detectCurrentPlatform(PLATFORM_STATUS_SIGNATURES)).resolves.toEqual({
      platform,
      evidence: [{ code: `browser.strong.status_schema.${platform}` }],
      system_name: undefined,
    });
    expect(paths).toEqual(["/api/status"]);
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

  it("recognizes a customized Done Hub from status and public group schemas", async () => {
    installPage();
    const requests: Array<{ path: string; method: string; credentials?: RequestCredentials }> = [];
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = new URL(String(input)).pathname;
      requests.push({
        path,
        method: init?.method ?? "GET",
        credentials: init?.credentials,
      });
      if (path === "/api/status") {
        return new Response(JSON.stringify({
          success: true,
          data: { linuxDo_oauth: true, max_log_query_days: 30, system_name: "Renamed Relay" },
        }), { status: 200 });
      }
      if (path === "/api/user_group_map") {
        return new Response(JSON.stringify({
          success: true,
          data: {
            default: { name: "default", symbol: "default", ratio: 1, dynamic_ratio: false },
            claude: { name: "Claude", symbol: "claude", ratio: 4, dynamic_ratio: true },
          },
        }), { status: 200 });
      }
      return new Response("{}", { status: 401 });
    }));

    await expect(detectCurrentPlatform(PLATFORM_STATUS_SIGNATURES)).resolves.toEqual({
      platform: "done-hub",
      evidence: [{ code: "browser.strong.status_group_schema.done-hub" }],
      system_name: "Renamed Relay",
    });
    expect(requests).toEqual([
      { path: "/api/status", method: "GET", credentials: "include" },
      { path: "/api/user_group_map", method: "GET", credentials: "omit" },
    ]);
    expect(requests.some((request) => request.path.startsWith("/api/token"))).toBe(false);
  });

  it.each([
    ["malformed", { default: { name: "default" } }],
    ["empty", {}],
  ] as const)("rejects a Done Hub partial status when the public group schema is %s", async (_case, groupData) => {
    installPage({}, "Done Hub compatible relay");
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const path = new URL(String(input)).pathname;
      if (path === "/api/status") {
        return new Response(JSON.stringify({
          success: true,
          data: { linuxDo_oauth: true, max_log_query_days: 30 },
        }), { status: 200 });
      }
      if (path === "/api/user_group_map") {
        return new Response(JSON.stringify({ success: true, data: groupData }), { status: 200 });
      }
      return new Response("{}", { status: 401 });
    }));

    const result = await detectCurrentPlatform(PLATFORM_STATUS_SIGNATURES);
    expect(result.platform).toBeUndefined();
    expect(result.reason).toBe("variant_inconclusive");
    expect(result.evidence).toEqual([{ code: "browser.weak.brand.done-hub" }]);
  });

  it("does not probe Done Hub groups when the status envelope is unsuccessful", async () => {
    installPage();
    const paths: string[] = [];
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const path = new URL(String(input)).pathname;
      paths.push(path);
      if (path === "/api/status") {
        return new Response(JSON.stringify({
          success: false,
          data: { linuxDo_oauth: true, max_log_query_days: 30 },
        }), { status: 200 });
      }
      return new Response("{}", { status: 401 });
    }));

    const result = await detectCurrentPlatform(PLATFORM_STATUS_SIGNATURES);
    expect(result.platform).toBeUndefined();
    expect(result.reason).toBe("not_logged_in");
    expect(paths).toEqual(["/api/status", "/api/user/self"]);
  });

  it("keeps a complete NewAPI signature ahead of the Done Hub variant path", async () => {
    installPage();
    const paths: string[] = [];
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      paths.push(new URL(String(input)).pathname);
      return new Response(JSON.stringify({
        success: true,
        data: {
          quota_display_type: "USD",
          passkey_login: true,
          setup: true,
          linuxDo_oauth: true,
          max_log_query_days: 30,
        },
      }), { status: 200 });
    }));

    const result = await detectCurrentPlatform(PLATFORM_STATUS_SIGNATURES);
    expect(result.platform).toBe("new-api");
    expect(paths).toEqual(["/api/status"]);
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
