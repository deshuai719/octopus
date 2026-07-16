package extensionupdater

import (
	"fmt"
	"os"
	"path/filepath"
)

type RuntimePaths struct {
	InstallDir   string
	Executable   string
	HostManifest string
	Config       string
}

func DefaultRuntimePaths() (RuntimePaths, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return RuntimePaths{}, fmt.Errorf("LOCALAPPDATA is unavailable")
	}
	installDir := filepath.Join(localAppData, "Octopus", "ExtensionUpdater")
	return RuntimePaths{
		InstallDir:   installDir,
		Executable:   filepath.Join(installDir, "octopus-extension-updater.exe"),
		HostManifest: filepath.Join(installDir, NativeHostName+".json"),
		Config:       filepath.Join(installDir, "config.json"),
	}, nil
}
