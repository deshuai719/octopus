import { describe, expect, it } from "vitest";
// @ts-expect-error Node built-in types are intentionally excluded from the extension runtime tsconfig.
import { readFileSync } from "node:fs";

const sidepanelCSS = readFileSync(new URL("../static/sidepanel.css", import.meta.url), "utf8");
const sidepanelHTML = readFileSync(new URL("../static/sidepanel.html", import.meta.url), "utf8");
const sidepanelScript = readFileSync(new URL("../src/sidepanel.ts", import.meta.url), "utf8");
const panelStateScript = readFileSync(new URL("../src/panel-state.ts", import.meta.url), "utf8");

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

  it("edits an existing account using the visible page title before saved identity fallbacks", () => {
    expect(sidepanelScript).toContain('accountNameInput.value = capture.page_title || capture.account_name || capture.candidate?.identity_label || "默认账号"');
    expect(sidepanelScript).toContain('["create_site_account", "create_account", "update_account"].includes(capture.action ?? "")');
    expect(sidepanelHTML).toContain('id="account-name" maxlength="128"');
  });

  it("uses the visible page title as the account-name default and reports legacy tag fallback", () => {
    expect(sidepanelScript).toContain("capture.page_title || capture.account_name");
    expect(sidepanelScript).toContain("后端版本暂不支持导入标签");
  });

  it("clears previous matched values before switching capture context", () => {
    expect(sidepanelScript).toContain('matchedSite.textContent = "—"');
    expect(sidepanelScript).toContain('matchedAccount.textContent = "—"');
    expect(sidepanelScript).toContain('siteNameInput.value = ""');
    expect(sidepanelScript).toContain('accountNameInput.value = ""');
    expect(sidepanelScript).toContain("clearTransferProgress();");
    expect(sidepanelScript).toContain("clearTransferReceipt();");
  });

  it("renders a four-step transfer progress and a non-sensitive result receipt", () => {
    for (const step of ["detected", "previewed", "saved", "synced"]) {
      expect(sidepanelHTML).toContain(`data-progress-step="${step}"`);
    }
    for (const field of ["receipt-action", "receipt-site", "receipt-account", "receipt-saved", "receipt-synced", "receipt-message"]) {
      expect(sidepanelHTML).toContain(`id="${field}"`);
    }
    expect(sidepanelScript).toContain("renderTransferProgress(capture.phase)");
    expect(sidepanelScript).toContain("renderTransferReceipt(capture)");
    expect(sidepanelScript).toContain("账号和凭据尚未写入");
  });

  it("renders session-scoped import summaries and additive site tags", () => {
    for (const field of ["import-summary", "summary-counts", "summary-list", "site-billing-tag", "site-custom-tags", "site-existing-tags"]) {
      expect(sidepanelHTML).toContain(`id="${field}"`);
    }
    expect(sidepanelScript).toContain('type: "get_direct_capture_summary"');
    expect(sidepanelScript).toContain('type: "open_direct_capture_origin"');
    expect(sidepanelScript).toContain('add_tags: [siteBillingTag.value, ...customTags]');
    expect(sidepanelScript).toContain('areaName !== "session"');
  });

  it("keeps validation failures on the real progress instead of a decorative rail", () => {
    expect(sidepanelHTML).toContain('<div class="rail" aria-hidden="true"></div>');
    expect(sidepanelHTML).not.toMatch(/class="rail"[^>]*>\s*<span/s);
    expect(sidepanelScript).toContain("function renderCaptureFailure");
    expect(sidepanelScript).toContain("renderCaptureFailure(response.message);");
    expect(sidepanelScript).toContain("if (!currentCapture && !transferProgress.hidden) renderCaptureFailure(event.message);");
  });

  it("contains no legacy recovery user flow", () => {
    for (const source of [sidepanelHTML, sidepanelScript, panelStateScript]) {
      expect(source).not.toMatch(/恢复页面|恢复会话|recovery capability/i);
    }
  });
});
