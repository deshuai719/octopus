package extensionupdater

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyReleaseDownloadsVerifiesAndReplaces(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	current := filepath.Join(root, "OctopusExtension")
	payload := filepath.Join(root, "payload")
	installDir := filepath.Join(root, "updater")
	writeTestExtension(t, current, "0.2.0")
	writeTestExtension(t, payload, "0.3.0")
	if err := os.MkdirAll(installDir, 0o700); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(root, "octopus-extension-0.3.0.zip")
	if err := createExtensionZip(payload, archivePath); err != nil {
		t.Fatal(err)
	}
	spec, err := fileAssetSpec(filepath.Base(archivePath), archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archiveBytes, _ := os.ReadFile(archivePath)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(archiveBytes)
	}))
	defer server.Close()
	release := &VerifiedRelease{
		Version:  "0.3.0",
		Manifest: UpdateManifest{Extension: spec},
		Assets:   map[string]githubAsset{spec.Asset: {Name: spec.Asset, BrowserDownloadURL: server.URL, Size: spec.Size}},
	}
	client := &ReleaseClient{httpClient: server.Client()}
	results, err := ApplyRelease(context.Background(), client, release, installDir, filepath.Join(installDir, "config.json"), []Target{{Browser: "Chrome", Profile: "Default", Path: current}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "updated" {
		t.Fatalf("results=%#v", results)
	}
	version, _ := extensionVersion(current)
	if version != "0.3.0" {
		t.Fatalf("version=%s", version)
	}
}

func TestReplaceAndRollbackTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	current := filepath.Join(root, "OctopusExtension")
	payload := filepath.Join(root, "payload")
	writeTestExtension(t, current, "0.2.0")
	writeTestExtension(t, payload, "0.3.0")
	if err := os.WriteFile(filepath.Join(payload, "worker.js"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup, err := replaceTarget(payload, current, "0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	version, _ := extensionVersion(current)
	if version != "0.3.0" {
		t.Fatalf("version=%s", version)
	}
	if err := rollbackTarget(current, backup); err != nil {
		t.Fatal(err)
	}
	version, _ = extensionVersion(current)
	if version != "0.2.0" {
		t.Fatalf("rollback version=%s", version)
	}
}

func TestExtractExtensionZipRejectsTraversal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	archivePath := filepath.Join(root, "bad.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, _ := writer.Create("../escape.txt")
	_, _ = entry.Write([]byte("bad"))
	_ = writer.Close()
	_ = file.Close()
	if err := extractExtensionZip(archivePath, filepath.Join(root, "out")); err == nil {
		t.Fatal("expected traversal ZIP to fail")
	}
}

func TestRollbackTargetsRejectsMissingBackup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "OctopusExtension")
	writeTestExtension(t, target, "0.3.0")
	configPath := filepath.Join(root, "config.json")
	if err := SaveConfig(configPath, Config{Targets: []Target{{Browser: "Chrome", Profile: "Default", Path: target}}}); err != nil {
		t.Fatal(err)
	}
	results, err := RollbackTargets(configPath, []Target{{Browser: "Chrome", Profile: "Default", Path: target}}, nil)
	if err == nil {
		t.Fatal("expected rollback without backup to fail")
	}
	if len(results) != 1 || results[0].Status != "no_backup" {
		t.Fatalf("results=%#v", results)
	}
}
