import { afterEach, describe, expect, it, vi } from "vitest";

import {
  canonicalHTTPOrigin,
  getOctopusBinding,
  octopusAPI,
  readOctopusPageAuth,
  validateOctopusBinding,
} from "../src/binding";
import type { OctopusBinding } from "../src/types";

function responseAt(url: string, body: string, init: ResponseInit = {}) {
  const response = new Response(body, init);
  Object.defineProperty(response, "url", { value: url });
  return response;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Octopus binding", () => {
  it("normalizes HTTP origins without retaining paths", () => {
    expect(canonicalHTTPOrigin("HTTPS://Example.COM:443/path?q=1")).toBe("https://example.com");
    expect(canonicalHTTPOrigin("file:///tmp/a")).toBeUndefined();
  });

  it("parses only the exact auth-storage fields", () => {
    vi.stubGlobal("localStorage", {
      getItem: () => JSON.stringify({ state: { token: "jwt-value", expireAt: "2099-01-01T00:00:00Z", isAPIKeyAuth: false } }),
    });
    expect(readOctopusPageAuth()).toEqual({ token: "jwt-value", expire_at: "2099-01-01T00:00:00Z", is_api_key_auth: false });
  });

  it("rejects a stored origin containing a path", async () => {
    vi.stubGlobal("chrome", { storage: { local: { get: vi.fn(async () => ({
      octopusBindingV1: {
        version: 1,
        origin: "https://octopus.example/path",
        token: "jwt-value",
        expire_at: "2099-01-01T00:00:00Z",
        validated_at: "2026-01-01T00:00:00Z",
      },
    })) } } });
    await expect(getOctopusBinding()).resolves.toBeUndefined();
  });

  it("requires the capability header and same-origin response", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => responseAt(
      "https://octopus.example/api/v1/user/status",
      "ok",
      { status: 200, headers: { "X-Octopus-Direct-Capture-Version": "1" } },
    )));
    await expect(validateOctopusBinding("https://octopus.example", "jwt-value")).resolves.toBeUndefined();

    vi.stubGlobal("fetch", vi.fn(async () => responseAt("https://other.example/status", "ok", {
      status: 200,
      headers: { "X-Octopus-Direct-Capture-Version": "1" },
    })));
    await expect(validateOctopusBinding("https://octopus.example", "jwt-value")).rejects.toThrow("跨站跳转");
  });

  it("pins authorization and operation headers after caller headers", async () => {
    const binding: OctopusBinding = {
      version: 1,
      origin: "https://octopus.example",
      token: "bound-jwt-value",
      expire_at: "2099-01-01T00:00:00Z",
      validated_at: "2026-01-01T00:00:00Z",
    };
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const headers = new Headers(init?.headers);
      expect(headers.get("Authorization")).toBe("Bearer bound-jwt-value");
      expect(headers.get("X-Octopus-Operation-ID")).toBe("550e8400-e29b-41d4-a716-446655440000");
      return responseAt("https://octopus.example/api/v1/site/direct-capture/capture", JSON.stringify({ data: { ok: true } }), { status: 200 });
    });
    vi.stubGlobal("fetch", fetchMock);
    await expect(octopusAPI(binding, "/api/v1/site/direct-capture/capture", "550e8400-e29b-41d4-a716-446655440000", {
      headers: { Authorization: "Bearer attacker-value", "X-Octopus-Operation-ID": "attacker-operation" },
    })).resolves.toEqual({ ok: true });
    await expect(octopusAPI(binding, "/api/v1/site/list", "550e8400-e29b-41d4-a716-446655440000")).rejects.toThrow("未授权");
    expect(fetchMock).toHaveBeenCalledOnce();
  });
});
