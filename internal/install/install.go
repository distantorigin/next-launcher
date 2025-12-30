package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorldFileConfig holds the server configuration for world files
type WorldFileConfig struct {
	DefaultServer string
	LocalServer   string
	ProxianiPort  string
	MUDMixerPort  string
}

// UpdateWorldFile updates a world file to use localhost instead of the default server
func UpdateWorldFile(worldFilePath string, updatePort bool, cfg WorldFileConfig) error {
	data, err := os.ReadFile(worldFilePath)
	if err != nil {
		return fmt.Errorf("failed to read world file: %w", err)
	}

	content := string(data)

	// Replace server with localhost
	updatedContent := strings.ReplaceAll(content, `site="`+cfg.DefaultServer+`"`, `site="`+cfg.LocalServer+`"`)

	// Update port for MUDMixer if requested
	if updatePort {
		updatedContent = strings.ReplaceAll(updatedContent, `port="`+cfg.ProxianiPort+`"`, `port="`+cfg.MUDMixerPort+`"`)
	}

	if updatedContent == content {
		return fmt.Errorf("no %s references found in world file", cfg.DefaultServer)
	}

	if err := os.WriteFile(worldFilePath, []byte(updatedContent), 0644); err != nil {
		return fmt.Errorf("failed to write world file: %w", err)
	}

	return nil
}

// CreateChannelSwitchBatchFiles creates batch files for switching update channels
func CreateChannelSwitchBatchFiles(installDir string) error {
	files := map[string]string{
		"Switch to Stable.bat":      "@echo off\nupdate.exe switch stable\n",
		"Switch to Dev.bat":         "@echo off\nupdate.exe switch dev\n",
		"Switch to Any Channel.bat": "@echo off\nupdate.exe switch\n",
	}

	for filename, content := range files {
		path := filepath.Join(installDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create %s: %w", filename, err)
		}
	}

	return nil
}

// IsInstalled checks if the current directory contains a valid installation
func IsInstalled(baseDir string) bool {
	hasMUSHclient := false
	hasWorlds := false
	hasManifest := false

	if _, err := os.Stat(filepath.Join(baseDir, "MUSHclient.exe")); err == nil {
		hasMUSHclient = true
	} else if _, err := os.Stat(filepath.Join(baseDir, "mushclient.exe")); err == nil {
		hasMUSHclient = true
	}

	if info, err := os.Stat(filepath.Join(baseDir, "worlds")); err == nil && info.IsDir() {
		hasWorlds = true
	}

	if _, err := os.Stat(filepath.Join(baseDir, ".manifest")); err == nil {
		hasManifest = true
	}

	return hasMUSHclient && (hasWorlds || hasManifest)
}

// HasWorldFiles checks if the directory contains world files
func HasWorldFiles(baseDir string) bool {
	if _, err := os.Stat(filepath.Join(baseDir, "MUSHclient.exe")); err == nil {
		return true
	}

	worldsDir := filepath.Join(baseDir, "worlds")
	if info, err := os.Stat(worldsDir); err != nil || !info.IsDir() {
		return false
	}

	entries, err := os.ReadDir(worldsDir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".mcl") {
			return true
		}
	}

	return false
}
