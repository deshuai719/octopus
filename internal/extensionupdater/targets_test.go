package extensionupdater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverTargetsDeduplicatesChromeAndEdge(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	extension := filepath.Join(root, "portable", "OctopusExtension")
	writeTestExtension(t, extension, "0.2.0")
	for _, browser := range []string{filepath.Join("Google", "Chrome"), filepath.Join("Microsoft", "Edge")} {
		profile := filepath.Join(root, browser, "User Data", "Default")
		if err := os.MkdirAll(profile, 0o700); err != nil {
			t.Fatal(err)
		}
		preferences := map[string]any{
			"extensions": map[string]any{
				"settings": map[string]any{
					ExtensionID: map[string]any{"path": extension, "location": 4, "from_webstore": false},
				},
			},
		}
		payload, _ := json.Marshal(preferences)
		if err := os.WriteFile(filepath.Join(profile, "Secure Preferences"), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	targets, err := DiscoverTargets(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets=%#v", targets)
	}
	if targets[0].Browser != "Chrome, Edge" {
		t.Fatalf("browser=%s", targets[0].Browser)
	}
}

func TestMergeTargetsDoesNotAccumulateBrowserLabels(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "OctopusExtension")
	merged := MergeTargets(
		[]Target{{Browser: "Chrome, Edge, Chrome, Edge", Profile: "Default", Path: path}},
		[]Target{{Browser: "Chrome, Edge", Profile: "Default", Path: path}},
	)
	if len(merged) != 1 || merged[0].Browser != "Chrome, Edge" {
		t.Fatalf("merged=%#v", merged)
	}
}

func writeTestExtension(t *testing.T, dir, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"manifest_version": 3,
		"name":             "Octopus",
		"version":          version,
		"key":              ManifestPublicKeyBase64,
	}
	payload, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
}
