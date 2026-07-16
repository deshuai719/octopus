package extensionupdater

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxReleaseListBytes  = 2 << 20
	maxManifestBytes     = 256 << 10
	maxSignatureBytes    = 4 << 10
	maxExtensionZipBytes = 64 << 20
)

type AssetSpec struct {
	Asset  string `json:"asset"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type UpdateManifest struct {
	SchemaVersion     int       `json:"schema_version"`
	Channel           string    `json:"channel"`
	Version           string    `json:"version"`
	ExtensionID       string    `json:"extension_id"`
	MinUpdaterVersion string    `json:"min_updater_version"`
	Extension         AssetSpec `json:"extension"`
	Updater           AssetSpec `json:"updater"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
}

type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []githubAsset `json:"assets"`
}

type VerifiedRelease struct {
	Version  string
	Manifest UpdateManifest
	Assets   map[string]githubAsset
}

type ReleaseClient struct {
	httpClient *http.Client
	apiURL     string
	publicKey  ed25519.PublicKey
}

func NewReleaseClient() (*ReleaseClient, error) {
	publicKey, err := base64.StdEncoding.DecodeString(ReleasePublicKeyBase64)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid embedded release public key")
	}
	return &ReleaseClient{
		httpClient: &http.Client{
			Timeout: 2 * time.Minute,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if len(via) > 5 {
					return fmt.Errorf("too many redirects")
				}
				if !allowedGitHubDownloadHost(request.URL.Hostname()) {
					return fmt.Errorf("redirect outside GitHub download hosts")
				}
				return nil
			},
		},
		apiURL:    fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=100", GitHubOwner, GitHubRepo),
		publicKey: ed25519.PublicKey(publicKey),
	}, nil
}

func (client *ReleaseClient) DownloadAsset(ctx context.Context, release *VerifiedRelease, spec AssetSpec, destination string) error {
	asset, ok := release.Assets[spec.Asset]
	if !ok {
		return fmt.Errorf("release asset %s is unavailable", spec.Asset)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "Octopus-Extension-Updater/"+UpdaterVersion)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	temporary := destination + ".part"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(response.Body, spec.Size+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(temporary)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return closeErr
	}
	if written != spec.Size {
		_ = os.Remove(temporary)
		return fmt.Errorf("asset %s size is %d, want %d", spec.Asset, written, spec.Size)
	}
	if !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), spec.SHA256) {
		_ = os.Remove(temporary)
		return fmt.Errorf("asset %s SHA-256 mismatch", spec.Asset)
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func (client *ReleaseClient) Check(ctx context.Context) (*VerifiedRelease, error) {
	releases, err := client.listReleases(ctx)
	if err != nil {
		return nil, err
	}
	release, version, err := selectExtensionRelease(releases)
	if err != nil {
		return nil, err
	}
	assets := make(map[string]githubAsset, len(release.Assets))
	for _, asset := range release.Assets {
		if _, exists := assets[asset.Name]; exists {
			return nil, fmt.Errorf("release %s contains duplicate asset %s", release.TagName, asset.Name)
		}
		if err := validateReleaseAssetURL(asset.BrowserDownloadURL); err != nil {
			return nil, fmt.Errorf("asset %s: %w", asset.Name, err)
		}
		assets[asset.Name] = asset
	}
	manifestAsset, ok := assets[ReleaseManifestAsset]
	if !ok {
		return nil, fmt.Errorf("release %s is missing %s", release.TagName, ReleaseManifestAsset)
	}
	signatureAsset, ok := assets[ReleaseSignatureAsset]
	if !ok {
		return nil, fmt.Errorf("release %s is missing %s", release.TagName, ReleaseSignatureAsset)
	}
	manifestBytes, err := client.download(ctx, manifestAsset.BrowserDownloadURL, maxManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("download update manifest: %w", err)
	}
	signatureBytes, err := client.download(ctx, signatureAsset.BrowserDownloadURL, maxSignatureBytes)
	if err != nil {
		return nil, fmt.Errorf("download update signature: %w", err)
	}
	manifest, err := verifyUpdateManifest(manifestBytes, signatureBytes, client.publicKey, version)
	if err != nil {
		return nil, err
	}
	for _, spec := range []AssetSpec{manifest.Extension, manifest.Updater} {
		asset, exists := assets[spec.Asset]
		if !exists {
			return nil, fmt.Errorf("signed manifest asset %s is missing from release", spec.Asset)
		}
		if asset.Size > 0 && asset.Size != spec.Size {
			return nil, fmt.Errorf("asset %s size differs from signed manifest", spec.Asset)
		}
		if asset.Digest != "" && !strings.EqualFold(asset.Digest, "sha256:"+spec.SHA256) {
			return nil, fmt.Errorf("asset %s digest differs from signed manifest", spec.Asset)
		}
	}
	return &VerifiedRelease{Version: version, Manifest: manifest, Assets: assets}, nil
}

func (client *ReleaseClient) listReleases(ctx context.Context) ([]githubRelease, error) {
	payload, err := client.download(ctx, client.apiURL, maxReleaseListBytes)
	if err != nil {
		return nil, fmt.Errorf("list GitHub releases: %w", err)
	}
	var releases []githubRelease
	if err := json.Unmarshal(payload, &releases); err != nil {
		return nil, fmt.Errorf("parse GitHub releases: %w", err)
	}
	return releases, nil
}

func (client *ReleaseClient) download(ctx context.Context, address string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Octopus-Extension-Updater/"+UpdaterVersion)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	reader := io.LimitReader(response.Body, limit+1)
	payload, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return payload, nil
}

func selectExtensionRelease(releases []githubRelease) (githubRelease, string, error) {
	var selected githubRelease
	selectedVersion := ""
	for _, release := range releases {
		if release.Draft || release.Prerelease || !strings.HasPrefix(release.TagName, ReleaseTagPrefix) {
			continue
		}
		version := strings.TrimPrefix(release.TagName, ReleaseTagPrefix)
		if _, err := parseVersion(version); err != nil {
			continue
		}
		hasManifest := false
		hasSignature := false
		for _, asset := range release.Assets {
			hasManifest = hasManifest || asset.Name == ReleaseManifestAsset
			hasSignature = hasSignature || asset.Name == ReleaseSignatureAsset
		}
		if !hasManifest || !hasSignature {
			continue
		}
		if selectedVersion == "" {
			selected, selectedVersion = release, version
			continue
		}
		comparison, _ := CompareVersions(version, selectedVersion)
		if comparison > 0 {
			selected, selectedVersion = release, version
		}
	}
	if selectedVersion == "" {
		return githubRelease{}, "", fmt.Errorf("no signed stable extension release found")
	}
	return selected, selectedVersion, nil
}

func verifyUpdateManifest(manifestBytes, encodedSignature []byte, publicKey ed25519.PublicKey, releaseVersion string) (UpdateManifest, error) {
	var manifest UpdateManifest
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encodedSignature)))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return manifest, fmt.Errorf("invalid update manifest signature encoding")
	}
	if !ed25519.Verify(publicKey, manifestBytes, signature) {
		return manifest, fmt.Errorf("update manifest signature is invalid")
	}
	if err := decodeStrictJSON(manifestBytes, &manifest); err != nil {
		return manifest, fmt.Errorf("parse signed update manifest: %w", err)
	}
	if err := validateUpdateManifest(manifest, releaseVersion); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func validateUpdateManifest(manifest UpdateManifest, releaseVersion string) error {
	if manifest.SchemaVersion != 1 || manifest.Channel != "stable" {
		return fmt.Errorf("unsupported update manifest schema or channel")
	}
	if manifest.Version != releaseVersion {
		return fmt.Errorf("manifest version %s differs from release %s", manifest.Version, releaseVersion)
	}
	if _, err := parseVersion(manifest.Version); err != nil {
		return err
	}
	if _, err := parseVersion(manifest.MinUpdaterVersion); err != nil {
		return err
	}
	if manifest.ExtensionID != ExtensionID {
		return fmt.Errorf("manifest extension id does not match Octopus")
	}
	wantExtensionAsset := fmt.Sprintf("octopus-extension-%s.zip", manifest.Version)
	if manifest.Extension.Asset != wantExtensionAsset {
		return fmt.Errorf("unexpected extension asset %s", manifest.Extension.Asset)
	}
	if manifest.Updater.Asset != "octopus-extension-updater-windows-amd64.exe" {
		return fmt.Errorf("unexpected updater asset %s", manifest.Updater.Asset)
	}
	if err := validateAssetSpec(manifest.Extension, maxExtensionZipBytes); err != nil {
		return fmt.Errorf("extension asset: %w", err)
	}
	if err := validateAssetSpec(manifest.Updater, 32<<20); err != nil {
		return fmt.Errorf("updater asset: %w", err)
	}
	return nil
}

func validateAssetSpec(spec AssetSpec, maximum int64) error {
	if spec.Asset == "" || spec.Size <= 0 || spec.Size > maximum {
		return fmt.Errorf("invalid asset name or size")
	}
	digest, err := hex.DecodeString(spec.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("invalid SHA-256")
	}
	return nil
}

func validateReleaseAssetURL(address string) error {
	parsed, err := url.Parse(address)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return fmt.Errorf("asset URL is outside github.com")
	}
	prefix := fmt.Sprintf("/%s/%s/releases/download/", GitHubOwner, GitHubRepo)
	if !strings.HasPrefix(parsed.EscapedPath(), prefix) {
		return fmt.Errorf("asset URL is outside the fixed Octopus repository")
	}
	return nil
}

func allowedGitHubDownloadHost(host string) bool {
	host = strings.ToLower(host)
	return host == "github.com" ||
		host == "api.github.com" ||
		host == "objects.githubusercontent.com" ||
		host == "release-assets.githubusercontent.com" ||
		strings.HasSuffix(host, ".githubusercontent.com")
}
