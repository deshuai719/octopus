import { describe, expect, it } from "vitest";
import manifest from "../manifest.json";

const EXPECTED_EXTENSION_ID = "hcnomejlhhefpnhljgcclhggoljokimn";

async function extensionIDFromPublicKey(publicKey: string): Promise<string> {
  const bytes = Uint8Array.from(atob(publicKey), (char) => char.charCodeAt(0));
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  return Array.from(digest.slice(0, 16), (byte) => (
    `${String.fromCharCode(97 + (byte >> 4))}${String.fromCharCode(97 + (byte & 0x0f))}`
  )).join("");
}

describe("manifest security policy", () => {
  it("keeps the published extension identity stable", async () => {
    expect(await extensionIDFromPublicKey(manifest.key)).toBe(EXPECTED_EXTENSION_ID);
    expect(manifest.version).toBe("0.3.3");
  });

  it("uses optional origins without permanent broad or debugger access", () => {
    expect(manifest).not.toHaveProperty("host_permissions");
    expect(manifest.permissions).not.toContain("debugger");
    expect(manifest.permissions).not.toContain("<all_urls>");
    expect(manifest.permissions).toEqual(expect.arrayContaining(["nativeMessaging", "downloads"]));
    expect(manifest.permissions).not.toContain("downloads.open");
    expect(manifest.optional_host_permissions).toEqual(["https://*/*", "http://*/*"]);
  });

  it("describes the direct-import-only workflow", () => {
    expect(manifest.description).toContain("直接创建或更新");
    expect(manifest.description).not.toContain("恢复");
  });
});
