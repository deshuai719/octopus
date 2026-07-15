import { describe, expect, it } from "vitest";
// @ts-expect-error Node built-in types are intentionally excluded from the extension runtime tsconfig.
import { readFileSync } from "node:fs";

const sidepanelCSS = readFileSync(new URL("../static/sidepanel.css", import.meta.url), "utf8");
const sidepanelHTML = readFileSync(new URL("../static/sidepanel.html", import.meta.url), "utf8");
const sidepanelScript = readFileSync(new URL("../src/sidepanel.ts", import.meta.url), "utf8");

describe("side panel visibility contract", () => {
  it("keeps hidden phase-specific controls out of layout", () => {
    expect(sidepanelCSS).toMatch(/\[hidden\]\s*\{[^}]*display:\s*none\s*!important;?[^}]*\}/s);
  });

  it("renders read-only matched site and account summaries from capture preview", () => {
    expect(sidepanelHTML).toContain('id="matched-site"');
    expect(sidepanelHTML).toContain('id="matched-account"');
    expect(sidepanelScript).toContain('matchedSite.textContent = capture.site_name || "—"');
    expect(sidepanelScript).toContain('matchedAccount.textContent = capture.account_name || capture.candidate?.identity_label || "—"');
  });

  it("edits an existing account using the Octopus-saved name as the default", () => {
    expect(sidepanelScript).toContain('accountNameInput.value = capture.account_name || capture.candidate?.identity_label || "默认账号"');
    expect(sidepanelScript).toContain('["create_site_account", "create_account", "update_account"].includes(capture.action ?? "")');
    expect(sidepanelHTML).toContain('id="account-name" maxlength="128"');
  });

  it("clears previous matched values before switching capture context", () => {
    expect(sidepanelScript).toContain('matchedSite.textContent = "—"');
    expect(sidepanelScript).toContain('matchedAccount.textContent = "—"');
    expect(sidepanelScript).toContain('siteNameInput.value = ""');
    expect(sidepanelScript).toContain('accountNameInput.value = ""');
  });
});
