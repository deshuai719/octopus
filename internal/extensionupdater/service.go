package extensionupdater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Service struct {
	paths   RuntimePaths
	release *ReleaseClient
}

type actionError struct {
	err     error
	targets []Target
}

func (err *actionError) Error() string { return err.err.Error() }
func (err *actionError) Unwrap() error { return err.err }

func NewService() (*Service, error) {
	paths, err := DefaultRuntimePaths()
	if err != nil {
		return nil, err
	}
	releaseClient, err := NewReleaseClient()
	if err != nil {
		return nil, err
	}
	return &Service{paths: paths, release: releaseClient}, nil
}

func Run(args []string, input io.Reader, output io.Writer) int {
	if isNativeInvocation(args) {
		service, err := NewService()
		if err != nil {
			return 1
		}
		if err := service.RunNativeHost(input, output); err != nil && !errors.Is(err, io.EOF) {
			return 1
		}
		return 0
	}
	if containsArgument(args, "--uninstall") {
		err := UninstallCurrentUser()
		if !containsArgument(args, "--silent") {
			ShowUninstallResult(err)
		}
		if err != nil {
			return 1
		}
		return 0
	}
	targets, err := InstallCurrentExecutable()
	if !containsArgument(args, "--install-silent") {
		ShowInstallResult(targets, err)
	}
	if err != nil {
		return 1
	}
	return 0
}

func (service *Service) RunNativeHost(input io.Reader, output io.Writer) error {
	for {
		var request Request
		if err := ReadMessage(input, &request); err != nil {
			return err
		}
		if err := service.dispatch(output, request); err != nil {
			var operationErr *actionError
			var targets []Target
			if errors.As(err, &operationErr) {
				targets = operationErr.targets
			}
			response := Response{
				ProtocolVersion: ProtocolVersion,
				RequestID:       request.RequestID,
				Kind:            "result",
				OK:              false,
				UpdaterVersion:  UpdaterVersion,
				Targets:         targets,
				Error:           &ResponseError{Code: errorCode(request.Action), Message: err.Error(), Retryable: isRetryable(err)},
			}
			if writeErr := WriteMessage(output, response); writeErr != nil {
				return writeErr
			}
		}
	}
}

func (service *Service) dispatch(output io.Writer, request Request) error {
	if request.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("unsupported native protocol version %d", request.ProtocolVersion)
	}
	if request.RequestID == "" || len(request.RequestID) > 128 {
		return fmt.Errorf("invalid request id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	targets, err := service.refreshTargets()
	if err != nil {
		return err
	}
	writeResult := func(response Response) error {
		response.ProtocolVersion = ProtocolVersion
		response.RequestID = request.RequestID
		response.Kind = "result"
		response.UpdaterVersion = UpdaterVersion
		return WriteMessage(output, response)
	}
	progress := func(stage string, progressTargets []Target) {
		_ = WriteMessage(output, Response{
			ProtocolVersion: ProtocolVersion,
			RequestID:       request.RequestID,
			Kind:            "progress",
			OK:              true,
			Stage:           stage,
			UpdaterVersion:  UpdaterVersion,
			CurrentVersion:  request.CurrentVersion,
			Targets:         progressTargets,
		})
	}

	switch request.Action {
	case "status":
		stage := "ready"
		if len(targets) == 0 {
			stage = "target_required"
		}
		return writeResult(Response{OK: true, Stage: stage, CurrentVersion: request.CurrentVersion, Targets: targets})
	case "check":
		release, err := service.release.Check(ctx)
		if err != nil {
			return err
		}
		comparison, err := CompareVersions(request.CurrentVersion, release.Version)
		if err != nil {
			return err
		}
		return writeResult(Response{OK: true, Stage: "checked", CurrentVersion: request.CurrentVersion, LatestVersion: release.Version, UpdateAvailable: comparison < 0, Targets: targets})
	case "update":
		release, err := service.release.Check(ctx)
		if err != nil {
			return err
		}
		comparison, err := CompareVersions(request.CurrentVersion, release.Version)
		if err != nil {
			return err
		}
		if comparison >= 0 {
			return writeResult(Response{OK: true, Stage: "up_to_date", CurrentVersion: request.CurrentVersion, LatestVersion: release.Version, Targets: targets})
		}
		updaterComparison, err := CompareVersions(UpdaterVersion, release.Manifest.MinUpdaterVersion)
		if err != nil {
			return err
		}
		if updaterComparison < 0 {
			if err := service.stageUpdaterUpgrade(ctx, release); err != nil {
				return err
			}
			return writeResult(Response{OK: true, Stage: "updater_restarting", CurrentVersion: request.CurrentVersion, LatestVersion: release.Version, Targets: targets})
		}
		results, err := ApplyRelease(ctx, service.release, release, service.paths.InstallDir, service.paths.Config, targets, progress)
		if err != nil {
			return &actionError{err: err, targets: results}
		}
		return writeResult(Response{OK: true, Stage: "completed", CurrentVersion: request.CurrentVersion, LatestVersion: release.Version, Targets: results})
	case "rollback":
		results, err := RollbackTargets(service.paths.Config, targets, progress)
		if err != nil {
			return &actionError{err: err, targets: results}
		}
		return writeResult(Response{OK: true, Stage: "rolled_back", CurrentVersion: request.CurrentVersion, Targets: results})
	case "select_target":
		initial := ""
		if len(targets) > 0 {
			initial = targets[0].Path
		}
		selected, ok, err := SelectExtensionManifest(initial)
		if err != nil {
			return err
		}
		if !ok {
			return writeResult(Response{OK: true, Stage: "selection_canceled", CurrentVersion: request.CurrentVersion, Targets: targets})
		}
		validated, err := ValidateExtensionDirectory(selected)
		if err != nil {
			return err
		}
		config, err := LoadConfig(service.paths.Config)
		if err != nil {
			return err
		}
		config.Targets = MergeTargets(config.Targets, []Target{{Browser: "Manual", Profile: "Selected", Path: validated}})
		if err := SaveConfig(service.paths.Config, config); err != nil {
			return err
		}
		return writeResult(Response{OK: true, Stage: "target_selected", CurrentVersion: request.CurrentVersion, Targets: config.Targets})
	default:
		return fmt.Errorf("unsupported updater action %q", request.Action)
	}
}

func (service *Service) stageUpdaterUpgrade(ctx context.Context, release *VerifiedRelease) error {
	versionDir := filepath.Join(service.paths.InstallDir, "versions", release.Manifest.MinUpdaterVersion)
	if err := os.MkdirAll(versionDir, 0o700); err != nil {
		return err
	}
	destination := filepath.Join(versionDir, "octopus-extension-updater.exe")
	if err := service.release.DownloadAsset(ctx, release, release.Manifest.Updater, destination); err != nil {
		return fmt.Errorf("download signed updater upgrade: %w", err)
	}
	if err := ActivateInstalledUpdater(destination); err != nil {
		return fmt.Errorf("activate signed updater upgrade: %w", err)
	}
	return nil
}

func (service *Service) refreshTargets() ([]Target, error) {
	config, err := LoadConfig(service.paths.Config)
	if err != nil {
		return nil, err
	}
	discovered, _ := DiscoverTargets(os.Getenv("LOCALAPPDATA"))
	config.Targets = MergeTargets(config.Targets, discovered)
	if err := SaveConfig(service.paths.Config, config); err != nil {
		return nil, err
	}
	return config.Targets, nil
}

func isNativeInvocation(args []string) bool {
	for _, argument := range args[1:] {
		if strings.EqualFold(strings.TrimRight(argument, "/"), strings.TrimRight(ExtensionOrigin, "/")) {
			return true
		}
	}
	return false
}

func containsArgument(args []string, wanted string) bool {
	for _, argument := range args[1:] {
		if argument == wanted {
			return true
		}
	}
	return false
}

func errorCode(action string) string {
	switch action {
	case "status":
		return "updater.status.failed"
	case "check":
		return "updater.check.failed"
	case "update":
		return "updater.update.failed"
	case "rollback":
		return "updater.rollback.failed"
	case "select_target":
		return "updater.target.failed"
	default:
		return "updater.request.invalid"
	}
}

func isRetryable(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "http ") || strings.Contains(message, "timeout") || strings.Contains(message, "temporar") || strings.Contains(message, "already running")
}

func InstalledUpdaterExists() bool {
	paths, err := DefaultRuntimePaths()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Clean(paths.Executable))
	return err == nil
}
