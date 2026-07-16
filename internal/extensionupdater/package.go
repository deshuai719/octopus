package extensionupdater

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func CreateReleaseArtifacts(distDir, outputDir, privateKeyPath string) (UpdateManifest, error) {
	dist, err := ValidateExtensionDirectory(distDir)
	if err != nil {
		return UpdateManifest{}, err
	}
	version, err := extensionVersion(dist)
	if err != nil {
		return UpdateManifest{}, err
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return UpdateManifest{}, err
	}
	extensionAsset := fmt.Sprintf("octopus-extension-%s.zip", version)
	extensionPath := filepath.Join(outputDir, extensionAsset)
	if err := createExtensionZip(dist, extensionPath); err != nil {
		return UpdateManifest{}, err
	}
	updaterSource := filepath.Join(dist, "octopus-extension-updater.exe")
	updaterAsset := "octopus-extension-updater-windows-amd64.exe"
	updaterPath := filepath.Join(outputDir, updaterAsset)
	if err := copyFile(updaterSource, updaterPath, 0o700); err != nil {
		return UpdateManifest{}, fmt.Errorf("copy updater release asset: %w", err)
	}
	extensionSpec, err := fileAssetSpec(extensionAsset, extensionPath)
	if err != nil {
		return UpdateManifest{}, err
	}
	updaterSpec, err := fileAssetSpec(updaterAsset, updaterPath)
	if err != nil {
		return UpdateManifest{}, err
	}
	manifest := UpdateManifest{
		SchemaVersion:     1,
		Channel:           "stable",
		Version:           version,
		ExtensionID:       ExtensionID,
		MinUpdaterVersion: UpdaterVersion,
		Extension:         extensionSpec,
		Updater:           updaterSpec,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return UpdateManifest{}, err
	}
	manifestBytes = append(manifestBytes, '\n')
	privateKey, err := readReleasePrivateKey(privateKeyPath)
	if err != nil {
		return UpdateManifest{}, err
	}
	signature := ed25519.Sign(privateKey, manifestBytes)
	if !ed25519.PublicKey(privateKey.Public().(ed25519.PublicKey)).Equal(mustReleasePublicKey()) {
		return UpdateManifest{}, fmt.Errorf("release private key does not match embedded public key")
	}
	encodedSignature := append([]byte(base64.StdEncoding.EncodeToString(signature)), '\n')
	if _, err := verifyUpdateManifest(manifestBytes, encodedSignature, mustReleasePublicKey(), version); err != nil {
		return UpdateManifest{}, fmt.Errorf("verify generated release manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, ReleaseManifestAsset), manifestBytes, 0o600); err != nil {
		return UpdateManifest{}, err
	}
	if err := os.WriteFile(filepath.Join(outputDir, ReleaseSignatureAsset), encodedSignature, 0o600); err != nil {
		return UpdateManifest{}, err
	}
	return manifest, nil
}

func createExtensionZip(source, destination string) error {
	entries := make([]string, 0)
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("extension dist contains symbolic link %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		entries = append(entries, path)
		return nil
	}); err != nil {
		return err
	}
	sort.Strings(entries)
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	fixedTime := time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, path := range entries {
		relative, err := filepath.Rel(source, path)
		if err != nil {
			writer.Close()
			file.Close()
			return err
		}
		header := &zip.FileHeader{Name: filepath.ToSlash(relative), Method: zip.Deflate}
		header.SetModTime(fixedTime)
		header.SetMode(0o600)
		entryWriter, err := writer.CreateHeader(header)
		if err != nil {
			writer.Close()
			file.Close()
			return err
		}
		sourceFile, err := os.Open(path)
		if err != nil {
			writer.Close()
			file.Close()
			return err
		}
		_, copyErr := io.Copy(entryWriter, sourceFile)
		closeErr := sourceFile.Close()
		if copyErr != nil {
			writer.Close()
			file.Close()
			return copyErr
		}
		if closeErr != nil {
			writer.Close()
			file.Close()
			return closeErr
		}
	}
	if err := writer.Close(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func fileAssetSpec(name, path string) (AssetSpec, error) {
	file, err := os.Open(path)
	if err != nil {
		return AssetSpec{}, err
	}
	hasher := sha256.New()
	size, copyErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if copyErr != nil {
		return AssetSpec{}, copyErr
	}
	if closeErr != nil {
		return AssetSpec{}, closeErr
	}
	return AssetSpec{Asset: name, SHA256: hex.EncodeToString(hasher.Sum(nil)), Size: size}, nil
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		input.Close()
		return err
	}
	_, copyErr := io.Copy(output, input)
	inputCloseErr := input.Close()
	outputCloseErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if inputCloseErr != nil {
		return inputCloseErr
	}
	return outputCloseErr
}

func readReleasePrivateKey(path string) (ed25519.PrivateKey, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(payload)
	if block == nil || !strings.Contains(block.Type, "PRIVATE KEY") {
		return nil, fmt.Errorf("release key is not a PEM private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("release key is not Ed25519")
	}
	return privateKey, nil
}

func mustReleasePublicKey() ed25519.PublicKey {
	value, err := base64.StdEncoding.DecodeString(ReleasePublicKeyBase64)
	if err != nil || len(value) != ed25519.PublicKeySize {
		panic("invalid embedded release public key")
	}
	return ed25519.PublicKey(value)
}
