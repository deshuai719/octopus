package extensionupdater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestKeyProducesFixedExtensionID(t *testing.T) {
	t.Parallel()
	id, err := ExtensionIDFromKeyBase64(ManifestPublicKeyBase64)
	if err != nil {
		t.Fatal(err)
	}
	if id != ExtensionID {
		t.Fatalf("id=%s, want %s", id, ExtensionID)
	}
}

func TestValidateExtensionDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifest := `{"manifest_version":3,"name":"Octopus","version":"0.2.0","key":"` + ManifestPublicKeyBase64 + `"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ValidateExtensionDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if got != want {
		t.Fatalf("path=%s, want %s", got, want)
	}
}
