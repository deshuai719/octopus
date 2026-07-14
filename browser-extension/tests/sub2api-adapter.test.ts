import { beforeEach, describe, expect, it, vi } from "vitest";
import { extractSub2APICredentials } from "../src/adapters/sub2api";

function storageWith(values: Record<string, string>): Storage {
  return {
    getItem: (key: string) => values[key] ?? null,
  } as Storage;
}

describe("Sub2API credential extraction", () => {
  beforeEach(() => {
    vi.stubGlobal("localStorage", storageWith({}));
    vi.stubGlobal("sessionStorage", storageWith({}));
  });

  it("reads the canonical credentials restored by the Sub2API home page", async () => {
    vi.stubGlobal("localStorage", storageWith({
      auth_token: "sub2api-access-token",
      refresh_token: "sub2api-refresh-token",
      token_expires_at: "4102444800000",
    }));

    await expect(extractSub2APICredentials()).resolves.toEqual({
      kind: "candidate",
      candidate: {
        access_token: "sub2api-access-token",
        refresh_token: "sub2api-refresh-token",
        token_expires_at: 4_102_444_800_000,
      },
    });
  });

  it("does not enumerate unrelated token-like storage keys", async () => {
    vi.stubGlobal("localStorage", storageWith({ api_token: "unapproved-token" }));

    await expect(extractSub2APICredentials()).resolves.toMatchObject({ kind: "not_logged_in" });
  });
});
