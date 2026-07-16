//go:build windows

package extensionupdater

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	mbOK              = 0x00000000
	mbIconInformation = 0x00000040
	mbIconError       = 0x00000010
	ofnPathMustExist  = 0x00000800
	ofnFileMustExist  = 0x00001000
	ofnNoChangeDir    = 0x00000008
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	messageBoxW      = user32.NewProc("MessageBoxW")
	comdlg32         = windows.NewLazySystemDLL("comdlg32.dll")
	getOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
)

type openFileName struct {
	StructSize       uint32
	Owner            uintptr
	Instance         uintptr
	Filter           *uint16
	CustomFilter     *uint16
	MaxCustomFilter  uint32
	FilterIndex      uint32
	File             *uint16
	MaxFile          uint32
	FileTitle        *uint16
	MaxFileTitle     uint32
	InitialDir       *uint16
	Title            *uint16
	Flags            uint32
	FileOffset       uint16
	FileExtension    uint16
	DefaultExtension *uint16
	CustomData       uintptr
	Hook             uintptr
	TemplateName     *uint16
	Reserved         uintptr
	ReservedValue    uint32
	FlagsEx          uint32
}

func InstallCurrentExecutable() ([]Target, error) {
	paths, err := DefaultRuntimePaths()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(paths.InstallDir, 0o700); err != nil {
		return nil, err
	}
	current, err := os.Executable()
	if err != nil {
		return nil, err
	}
	current, err = filepath.Abs(current)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(filepath.Clean(current), filepath.Clean(paths.Executable)) {
		if err := copyExecutable(current, paths.Executable); err != nil {
			return nil, err
		}
	}
	if err := writeNativeHostManifest(paths.HostManifest, paths.Executable); err != nil {
		return nil, err
	}
	if err := registerNativeHost(paths.HostManifest); err != nil {
		return nil, err
	}
	config, err := LoadConfig(paths.Config)
	if err != nil {
		return nil, err
	}
	discovered, discoverErr := DiscoverTargets(os.Getenv("LOCALAPPDATA"))
	config.Targets = MergeTargets(config.Targets, discovered)
	if err := SaveConfig(paths.Config, config); err != nil {
		return nil, err
	}
	if len(config.Targets) == 0 && discoverErr != nil {
		return nil, discoverErr
	}
	return config.Targets, nil
}

func ActivateInstalledUpdater(executable string) error {
	paths, err := DefaultRuntimePaths()
	if err != nil {
		return err
	}
	validated, err := filepath.Abs(executable)
	if err != nil {
		return err
	}
	info, err := os.Stat(validated)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("updater executable path is a directory")
	}
	if err := writeNativeHostManifest(paths.HostManifest, validated); err != nil {
		return err
	}
	return registerNativeHost(paths.HostManifest)
}

func UninstallCurrentUser() error {
	for _, path := range nativeRegistryPaths() {
		_ = registry.DeleteKey(registry.CURRENT_USER, path)
	}
	paths, err := DefaultRuntimePaths()
	if err != nil {
		return err
	}
	_ = os.Remove(paths.HostManifest)
	_ = os.Remove(paths.Config)
	return nil
}

func ShowUninstallResult(uninstallErr error) {
	if uninstallErr != nil {
		showMessage("Octopus 扩展更新助手", "移除更新支持失败：\n"+uninstallErr.Error(), mbOK|mbIconError)
		return
	}
	showMessage("Octopus 扩展更新助手", "Chrome 与 Edge 的自动更新注册已经移除。\n\n已安装的 Octopus 扩展目录不会被删除。", mbOK|mbIconInformation)
}

func SelectExtensionManifest(initialDir string) (string, bool, error) {
	buffer := make([]uint16, 32768)
	filterBuffer := utf16.Encode([]rune("Octopus manifest.json\x00manifest.json\x00JSON files\x00*.json\x00\x00"))
	filter := &filterBuffer[0]
	title, _ := syscall.UTF16PtrFromString("选择 Octopus 扩展目录中的 manifest.json")
	var initial *uint16
	if initialDir != "" {
		initial, _ = syscall.UTF16PtrFromString(initialDir)
	}
	dialog := openFileName{
		StructSize:  uint32(unsafe.Sizeof(openFileName{})),
		Filter:      filter,
		FilterIndex: 1,
		File:        &buffer[0],
		MaxFile:     uint32(len(buffer)),
		InitialDir:  initial,
		Title:       title,
		Flags:       ofnPathMustExist | ofnFileMustExist | ofnNoChangeDir,
	}
	result, _, callErr := getOpenFileNameW.Call(uintptr(unsafe.Pointer(&dialog)))
	if result == 0 {
		if callErr != windows.ERROR_SUCCESS {
			return "", false, callErr
		}
		return "", false, nil
	}
	selected := syscall.UTF16ToString(buffer)
	if !strings.EqualFold(filepath.Base(selected), "manifest.json") {
		return "", false, fmt.Errorf("selected file is not manifest.json")
	}
	return filepath.Dir(selected), true, nil
}

func ShowInstallResult(targets []Target, installErr error) {
	if installErr != nil {
		showMessage("Octopus 扩展更新助手", "初始化失败：\n"+installErr.Error(), mbOK|mbIconError)
		return
	}
	message := "自动更新支持已启用。\n\n请返回 Chrome 或 Edge 的 Octopus 扩展侧边栏继续更新。"
	if len(targets) == 0 {
		message += "\n\n尚未自动识别扩展目录，扩展会引导你选择 manifest.json。"
	}
	showMessage("Octopus 扩展更新助手", message, mbOK|mbIconInformation)
}

func showMessage(title, message string, flags uintptr) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messagePtr, _ := syscall.UTF16PtrFromString(message)
	_, _, _ = messageBoxW.Call(0, uintptr(unsafe.Pointer(messagePtr)), uintptr(unsafe.Pointer(titlePtr)), flags)
}

func copyExecutable(source, destination string) error {
	payload, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	temporary := destination + ".new"
	if err := os.WriteFile(temporary, payload, 0o700); err != nil {
		return err
	}
	_ = os.Remove(destination)
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func writeNativeHostManifest(path, executable string) error {
	manifest := struct {
		Name           string   `json:"name"`
		Description    string   `json:"description"`
		Path           string   `json:"path"`
		Type           string   `json:"type"`
		AllowedOrigins []string `json:"allowed_origins"`
	}{
		Name:           NativeHostName,
		Description:    "Octopus browser extension updater",
		Path:           executable,
		Type:           "stdio",
		AllowedOrigins: []string{ExtensionOrigin},
	}
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o600)
}

func registerNativeHost(manifestPath string) error {
	for _, path := range nativeRegistryPaths() {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
		if err != nil {
			return err
		}
		setErr := key.SetStringValue("", manifestPath)
		closeErr := key.Close()
		if setErr != nil {
			return setErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func nativeRegistryPaths() []string {
	return []string{
		`Software\Google\Chrome\NativeMessagingHosts\` + NativeHostName,
		`Software\Microsoft\Edge\NativeMessagingHosts\` + NativeHostName,
	}
}
