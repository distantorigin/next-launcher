package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// Normalize converts a path to use forward slashes (for manifest/cross-platform storage)
func Normalize(p string) string {
	return strings.ReplaceAll(filepath.Clean(p), string(filepath.Separator), "/")
}

// Denormalize converts a path from forward slashes to platform-specific separators
func Denormalize(p string) string {
	return strings.ReplaceAll(p, "/", string(filepath.Separator))
}

// FindActual finds the actual case of a file on case-insensitive filesystems
func FindActual(targetPath string) (string, error) {
	if _, err := os.Stat(targetPath); err == nil {
		return targetPath, nil
	}

	dir := filepath.Dir(targetPath)
	filename := filepath.Base(targetPath)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return targetPath, nil
	}

	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), filename) {
			return filepath.Join(dir, entry.Name()), nil
		}
	}

	return targetPath, nil
}

// IsUserConfig checks if a path is a user configuration file that should be preserved
func IsUserConfig(path string) bool {
	normalizedPath := strings.ToLower(Normalize(path))

	// User configuration files that should never be overwritten
	userFiles := []string{
		"mushclient_prefs.sqlite",
		"mushclient.ini",
	}

	for _, userFile := range userFiles {
		if normalizedPath == userFile {
			return true
		}
	}

	// World files, plugin state, logs, settings
	if strings.HasPrefix(normalizedPath, "worlds/") && strings.HasSuffix(normalizedPath, ".mcl") {
		return true
	}
	if strings.HasPrefix(normalizedPath, "worlds/plugins/state/") {
		return true
	}
	if strings.HasPrefix(normalizedPath, "logs/") {
		return true
	}
	if strings.HasPrefix(normalizedPath, "worlds/settings/") {
		return true
	}

	return false
}
