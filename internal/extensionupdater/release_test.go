package extensionupdater

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestGitHubReleasePayloadAllowsUnrelatedFields(t *testing.T) {
	t.Parallel()
	payload := []byte(`[{"tag_name":"extension-v0.3.0","draft":false,"prerelease":false,"html_url":"https://example.invalid","assets":[]}]`)
	var releases []githubRelease
	if err := json.Unmarshal(payload, &releases); err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 || releases[0].TagName != "extension-v0.3.0" {
		t.Fatalf("releases=%#v", releases)
	}
}

func TestSelectExtensionReleaseIgnoresServerAndPrerelease(t *testing.T) {
	t.Parallel()
	releases := []githubRelease{
		{TagName: "v0.8.40"},
		{TagName: "extension-v0.4.0", Prerelease: true, Assets: signedAssetNames()},
		{TagName: "extension-v0.2.0", Assets: signedAssetNames()},
		{TagName: "extension-v0.3.0", Assets: signedAssetNames()},
	}
	selected, version, err := selectExtensionRelease(releases)
	if err != nil {
		t.Fatal(err)
	}
	if selected.TagName != "extension-v0.3.0" || version != "0.3.0" {
		t.Fatalf("selected %s (%s)", selected.TagName, version)
	}
}

func TestVerifyUpdateManifest(t *testing.T) {
	t.Parallel()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := UpdateManifest{
		SchemaVersion:     1,
		Channel:           "stable",
		Version:           "0.3.0",
		ExtensionID:       ExtensionID,
		MinUpdaterVersion: "0.1.0",
		Extension:         AssetSpec{Asset: "octopus-extension-0.3.0.zip", SHA256: zeroDigest(), Size: 100},
		Updater:           AssetSpec{Asset: "octopus-extension-updater-windows-amd64.exe", SHA256: zeroDigest(), Size: 100},
	}
	payload, _ := json.Marshal(manifest)
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	got, err := verifyUpdateManifest(payload, []byte(signature), publicKey, "0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != manifest.Version {
		t.Fatalf("version=%s", got.Version)
	}
	payload[0] ^= 1
	if _, err := verifyUpdateManifest(payload, []byte(signature), publicKey, "0.3.0"); err == nil {
		t.Fatal("expected tampered manifest to fail")
	}
}

func signedAssetNames() []githubAsset {
	return []githubAsset{{Name: ReleaseManifestAsset}, {Name: ReleaseSignatureAsset}}
}

func zeroDigest() string {
	return "0000000000000000000000000000000000000000000000000000000000000000"
}
