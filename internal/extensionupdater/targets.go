package extensionupdater

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Config struct {
	SchemaVersion int               `json:"schema_version"`
	Targets       []Target          `json:"targets"`
	Backups       map[string]string `json:"backups,omitempty"`
}

type browserRoot struct {
	name string
	path string
}

func DiscoverTargets(localAppData string) ([]Target, error) {
	if strings.TrimSpace(localAppData) == "" {
		return nil, fmt.Errorf("LOCALAPPDATA is unavailable")
	}
	roots := []browserRoot{
		{name: "Chrome", path: filepath.Join(localAppData, "Google", "Chrome", "User Data")},
		{name: "Edge", path: filepath.Join(localAppData, "Microsoft", "Edge", "User Data")},
	}
	unique := make(map[string]Target)
	var parseErrors []error
	for _, root := range roots {
		profiles, err := os.ReadDir(root.path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			parseErrors = append(parseErrors, fmt.Errorf("read %s profiles: %w", root.name, err))
			continue
		}
		for _, profile := range profiles {
			if !profile.IsDir() || (profile.Name() != "Default" && !strings.HasPrefix(profile.Name(), "Profile ")) {
				continue
			}
			for _, filename := range []string{"Secure Preferences", "Preferences"} {
				candidate, found, err := readPreferenceTarget(filepath.Join(root.path, profile.Name(), filename))
				if err != nil {
					parseErrors = append(parseErrors, fmt.Errorf("parse %s %s: %w", root.name, filename, err))
					continue
				}
				if !found {
					continue
				}
				validated, err := ValidateExtensionDirectory(candidate)
				if err != nil {
					parseErrors = append(parseErrors, fmt.Errorf("validate %s %s target: %w", root.name, profile.Name(), err))
					continue
				}
				key := strings.ToLower(validated)
				existing, exists := unique[key]
				if exists {
					existing.Browser = mergeLabels(existing.Browser, root.name)
					existing.Profile = mergeLabels(existing.Profile, profile.Name())
					unique[key] = existing
				} else {
					unique[key] = Target{Browser: root.name, Profile: profile.Name(), Path: validated}
				}
			}
		}
	}
	targets := make([]Target, 0, len(unique))
	for _, target := range unique {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool { return strings.ToLower(targets[i].Path) < strings.ToLower(targets[j].Path) })
	if len(targets) == 0 && len(parseErrors) > 0 {
		return nil, errors.Join(parseErrors...)
	}
	return targets, nil
}

func readPreferenceTarget(path string) (string, bool, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	var preferences struct {
		Extensions struct {
			Settings map[string]struct {
				Path         string `json:"path"`
				Location     int    `json:"location"`
				FromWebstore bool   `json:"from_webstore"`
			} `json:"settings"`
		} `json:"extensions"`
	}
	if err := json.Unmarshal(payload, &preferences); err != nil {
		return "", false, err
	}
	entry, found := preferences.Extensions.Settings[ExtensionID]
	if !found || entry.Path == "" || entry.Location != 4 || entry.FromWebstore {
		return "", false, nil
	}
	if !filepath.IsAbs(entry.Path) {
		return "", false, fmt.Errorf("unpacked extension path is not absolute")
	}
	return entry.Path, true, nil
}

func mergeLabels(current, next string) string {
	labels := make([]string, 0)
	seen := make(map[string]struct{})
	for _, value := range []string{current, next} {
		for _, item := range strings.Split(value, ", ") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			key := strings.ToLower(item)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			labels = append(labels, item)
		}
	}
	return strings.Join(labels, ", ")
}

func LoadConfig(path string) (Config, error) {
	config := Config{SchemaVersion: 1, Backups: map[string]string{}}
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config, nil
		}
		return config, err
	}
	if err := decodeStrictJSON(payload, &config); err != nil {
		return Config{}, err
	}
	if config.SchemaVersion != 1 {
		return Config{}, fmt.Errorf("unsupported updater config schema %d", config.SchemaVersion)
	}
	if config.Backups == nil {
		config.Backups = map[string]string{}
	}
	valid := make([]Target, 0, len(config.Targets))
	seen := map[string]struct{}{}
	for _, target := range config.Targets {
		path, err := ValidateExtensionDirectory(target.Path)
		if err != nil {
			continue
		}
		key := strings.ToLower(path)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		target.Path = path
		valid = append(valid, target)
	}
	config.Targets = valid
	return config, nil
}

func SaveConfig(path string, config Config) error {
	config.SchemaVersion = 1
	if config.Backups == nil {
		config.Backups = map[string]string{}
	}
	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(payload, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func MergeTargets(current, discovered []Target) []Target {
	byPath := make(map[string]Target)
	for _, target := range append(append([]Target{}, current...), discovered...) {
		if target.Path == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(target.Path))
		if existing, exists := byPath[key]; exists {
			existing.Browser = mergeLabels(existing.Browser, target.Browser)
			existing.Profile = mergeLabels(existing.Profile, target.Profile)
			byPath[key] = existing
		} else {
			target.Path = filepath.Clean(target.Path)
			byPath[key] = target
		}
	}
	result := make([]Target, 0, len(byPath))
	for _, target := range byPath {
		result = append(result, target)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].Path) < strings.ToLower(result[j].Path) })
	return result
}
