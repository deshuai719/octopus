//go:build !windows

package extensionupdater

import "fmt"

func InstallCurrentExecutable() ([]Target, error) {
	return nil, fmt.Errorf("Octopus extension updater installation is supported on Windows only")
}

func UninstallCurrentUser() error {
	return fmt.Errorf("Octopus extension updater uninstallation is supported on Windows only")
}

func ActivateInstalledUpdater(string) error {
	return fmt.Errorf("updater activation is supported on Windows only")
}

func SelectExtensionManifest(string) (string, bool, error) {
	return "", false, fmt.Errorf("extension directory selection is supported on Windows only")
}

func ShowInstallResult([]Target, error) {}

func ShowUninstallResult(error) {}
