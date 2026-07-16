package extensionupdater

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxZipFiles             = 1024
	maxZipUncompressedBytes = 128 << 20
)

type ProgressFunc func(stage string, targets []Target)

func ApplyRelease(
	ctx context.Context,
	client *ReleaseClient,
	release *VerifiedRelease,
	installDir string,
	configPath string,
	targets []Target,
	progress ProgressFunc,
) ([]Target, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("no verified unpacked extension targets")
	}
	unlock, err := acquireUpdateLock(filepath.Join(installDir, "update.lock"))
	if err != nil {
		return nil, err
	}
	defer unlock()

	if progress != nil {
		progress("downloading", targets)
	}
	payloadRoot, cleanup, err := preparePayload(ctx, client, release, installDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if progress != nil {
		progress("verified", targets)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load updater config: %w", err)
	}
	config.Targets = MergeTargets(config.Targets, targets)
	results := make([]Target, 0, len(targets))
	var updateErrors []error
	for _, target := range targets {
		updated := target
		if progress != nil {
			progress("replacing", []Target{target})
		}
		backup, err := replaceTarget(payloadRoot, target.Path, release.Version)
		if err != nil {
			updated.Status = "failed"
			updateErrors = append(updateErrors, fmt.Errorf("%s: %w", target.Path, err))
			results = append(results, updated)
			continue
		}
		key := strings.ToLower(filepath.Clean(target.Path))
		previous := config.Backups[key]
		config.Backups[key] = backup
		if err := SaveConfig(configPath, config); err != nil {
			config.Backups[key] = previous
			rollbackErr := rollbackTarget(target.Path, backup)
			updated.Status = "failed"
			results = append(results, updated)
			if rollbackErr != nil {
				updateErrors = append(updateErrors, fmt.Errorf("save backup metadata for %s: %v; restore current version: %w", target.Path, err, rollbackErr))
			} else {
				updateErrors = append(updateErrors, fmt.Errorf("save backup metadata for %s: %w", target.Path, err))
			}
			continue
		}
		updated.Status = "updated"
		results = append(results, updated)
		if previous != "" && previous != backup {
			_ = os.RemoveAll(previous)
		}
	}
	if progress != nil {
		progress("completed", results)
	}
	if len(updateErrors) > 0 {
		return results, errors.Join(updateErrors...)
	}
	return results, nil
}

func RollbackTargets(configPath string, targets []Target, progress ProgressFunc) ([]Target, error) {
	config, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	results := make([]Target, 0, len(targets))
	var rollbackErrors []error
	rolledBack := 0
	for _, target := range targets {
		result := target
		key := strings.ToLower(filepath.Clean(target.Path))
		backup := config.Backups[key]
		if backup == "" {
			result.Status = "no_backup"
			results = append(results, result)
			continue
		}
		if progress != nil {
			progress("rolling_back", []Target{target})
		}
		if err := rollbackTarget(target.Path, backup); err != nil {
			result.Status = "failed"
			rollbackErrors = append(rollbackErrors, fmt.Errorf("%s: %w", target.Path, err))
		} else {
			result.Status = "rolled_back"
			rolledBack++
			delete(config.Backups, key)
		}
		results = append(results, result)
	}
	if err := SaveConfig(configPath, config); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}
	if len(rollbackErrors) > 0 {
		return results, errors.Join(rollbackErrors...)
	}
	if rolledBack == 0 {
		return results, fmt.Errorf("no extension backup is available")
	}
	return results, nil
}

func preparePayload(ctx context.Context, client *ReleaseClient, release *VerifiedRelease, installDir string) (string, func(), error) {
	working, err := os.MkdirTemp(installDir, "payload-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(working) }
	archivePath := filepath.Join(working, release.Manifest.Extension.Asset)
	if err := client.DownloadAsset(ctx, release, release.Manifest.Extension, archivePath); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("download extension package: %w", err)
	}
	payloadRoot := filepath.Join(working, "extension")
	if err := extractExtensionZip(archivePath, payloadRoot); err != nil {
		cleanup()
		return "", nil, err
	}
	if _, err := ValidateExtensionDirectory(payloadRoot); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("validate staged extension: %w", err)
	}
	version, err := extensionVersion(payloadRoot)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if version != release.Version {
		cleanup()
		return "", nil, fmt.Errorf("staged extension version %s differs from release %s", version, release.Version)
	}
	return payloadRoot, cleanup, nil
}

func extractExtensionZip(archivePath, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open extension ZIP: %w", err)
	}
	defer reader.Close()
	if len(reader.File) == 0 || len(reader.File) > maxZipFiles {
		return fmt.Errorf("extension ZIP contains an invalid number of files")
	}
	var total uint64
	for _, file := range reader.File {
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("extension ZIP contains a symbolic link")
		}
		total += file.UncompressedSize64
		if total > maxZipUncompressedBytes {
			return fmt.Errorf("extension ZIP exceeds uncompressed size limit")
		}
		cleanName := filepath.Clean(filepath.FromSlash(file.Name))
		if cleanName == "." || filepath.IsAbs(cleanName) || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
			return fmt.Errorf("extension ZIP contains unsafe path %q", file.Name)
		}
		target := filepath.Join(destination, cleanName)
		relative, err := filepath.Rel(destination, target)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("extension ZIP path escapes destination")
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		destinationFile, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(destinationFile, source)
		closeDestinationErr := destinationFile.Close()
		closeSourceErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeDestinationErr != nil {
			return closeDestinationErr
		}
		if closeSourceErr != nil {
			return closeSourceErr
		}
	}
	return nil
}

func replaceTarget(payloadRoot, targetPath, version string) (string, error) {
	target, err := ValidateExtensionDirectory(targetPath)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(target)
	base := filepath.Base(target)
	suffix, err := randomSuffix()
	if err != nil {
		return "", err
	}
	staging := filepath.Join(parent, "."+base+".octopus-staging-"+suffix)
	backup := filepath.Join(parent, "."+base+".octopus-backup-"+time.Now().UTC().Format("20060102T150405Z")+"-"+suffix)
	if err := copyTree(payloadRoot, staging); err != nil {
		_ = os.RemoveAll(staging)
		return "", fmt.Errorf("create staging directory: %w", err)
	}
	stagedVersion, err := extensionVersion(staging)
	if err != nil || stagedVersion != version {
		_ = os.RemoveAll(staging)
		return "", fmt.Errorf("staged copy validation failed")
	}
	if err := os.Rename(target, backup); err != nil {
		_ = os.RemoveAll(staging)
		return "", fmt.Errorf("move current extension to backup: %w", err)
	}
	if err := os.Rename(staging, target); err != nil {
		restoreErr := os.Rename(backup, target)
		_ = os.RemoveAll(staging)
		if restoreErr != nil {
			return "", fmt.Errorf("activate update: %v; restore backup: %w", err, restoreErr)
		}
		return "", fmt.Errorf("activate update: %w", err)
	}
	return backup, nil
}

func rollbackTarget(targetPath, backupPath string) error {
	backup, err := ValidateExtensionDirectory(backupPath)
	if err != nil {
		return fmt.Errorf("validate backup: %w", err)
	}
	target := filepath.Clean(targetPath)
	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	failed := filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".octopus-failed-"+suffix)
	if err := os.Rename(target, failed); err != nil {
		return fmt.Errorf("move current extension aside: %w", err)
	}
	if err := os.Rename(backup, target); err != nil {
		restoreErr := os.Rename(failed, target)
		if restoreErr != nil {
			return fmt.Errorf("restore backup: %v; restore current: %w", err, restoreErr)
		}
		return fmt.Errorf("restore backup: %w", err)
	}
	_ = os.RemoveAll(failed)
	return nil
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("source contains symbolic link %s", relative)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			sourceFile.Close()
			return err
		}
		targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			sourceFile.Close()
			return err
		}
		_, copyErr := io.Copy(targetFile, sourceFile)
		closeSourceErr := sourceFile.Close()
		closeErr := targetFile.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeSourceErr != nil {
			return closeSourceErr
		}
		return closeErr
	})
}

func extensionVersion(path string) (string, error) {
	payload, err := os.ReadFile(filepath.Join(path, "manifest.json"))
	if err != nil {
		return "", err
	}
	var manifest ExtensionManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return "", err
	}
	if _, err := parseVersion(manifest.Version); err != nil {
		return "", err
	}
	return manifest.Version, nil
}

func acquireUpdateLock(path string) (func(), error) {
	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > 30*time.Minute {
		_ = os.Remove(path)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("another extension update is already running")
		}
		return nil, err
	}
	_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
	_ = file.Close()
	return func() { _ = os.Remove(path) }, nil
}

func randomSuffix() (string, error) {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
