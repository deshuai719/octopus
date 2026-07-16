package extensionupdater

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ExtensionManifest struct {
	ManifestVersion int    `json:"manifest_version"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	Key             string `json:"key"`
}

func ExtensionIDFromKeyBase64(value string) (string, error) {
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("decode manifest key: %w", err)
	}
	digest := sha256.Sum256(der)
	const alphabet = "abcdefghijklmnop"
	id := make([]byte, 32)
	for index, value := range digest[:16] {
		id[index*2] = alphabet[value>>4]
		id[index*2+1] = alphabet[value&0x0f]
	}
	return string(id), nil
}

func ValidateExtensionDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve extension path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve extension symlinks: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat extension path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("extension path is not a directory")
	}

	manifestBytes, err := os.ReadFile(filepath.Join(resolved, "manifest.json"))
	if err != nil {
		return "", fmt.Errorf("read extension manifest: %w", err)
	}
	var manifest ExtensionManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return "", fmt.Errorf("parse extension manifest: %w", err)
	}
	if manifest.ManifestVersion != 3 {
		return "", fmt.Errorf("unexpected manifest version %d", manifest.ManifestVersion)
	}
	if _, err := parseVersion(manifest.Version); err != nil {
		return "", err
	}
	wantKey, _ := base64.StdEncoding.DecodeString(ManifestPublicKeyBase64)
	gotKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(manifest.Key))
	if err != nil {
		return "", fmt.Errorf("decode extension manifest key: %w", err)
	}
	if len(gotKey) != len(wantKey) || subtle.ConstantTimeCompare(gotKey, wantKey) != 1 {
		return "", fmt.Errorf("extension manifest key does not match Octopus")
	}
	id, err := ExtensionIDFromKeyBase64(manifest.Key)
	if err != nil {
		return "", err
	}
	if id != ExtensionID {
		return "", fmt.Errorf("unexpected extension id %s", id)
	}
	return filepath.Clean(resolved), nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("unexpected trailing JSON")
	} else if err != io.EOF {
		return err
	}
	return nil
}
