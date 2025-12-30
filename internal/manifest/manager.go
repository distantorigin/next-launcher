package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileInfo represents a file in the manifest
type FileInfo struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
	URL  string `json:"url"`
}

// TreeItem represents a file in the Git tree
type TreeItem struct {
	Path string
	Type string
	SHA  string
}

// Config holds configuration for manifest operations
type Config struct {
	ManifestFile string
	WorldsDir    string
	WorldFileExt string
	ChannelFlag  string
	QuietFlag    bool
	VerboseFlag  bool
}

// Manager handles manifest operations
type Manager struct {
	config Config
}

// NewManager creates a new manifest manager
func NewManager(config Config) *Manager {
	return &Manager{
		config: config,
	}
}

// LoadLocal loads the local manifest file
func (m *Manager) LoadLocal() (map[string]FileInfo, error) {
	baseDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}
	path := filepath.Join(baseDir, m.config.ManifestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read local manifest: %w", err)
	}

	// Strip comment lines (lines starting with //) before parsing JSON
	lines := strings.Split(string(data), "\n")
	var jsonLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip lines that start with // (comments)
		if !strings.HasPrefix(trimmed, "//") {
			jsonLines = append(jsonLines, line)
		}
	}
	cleanedData := strings.Join(jsonLines, "\n")

	var manifest map[string]FileInfo
	if err := json.Unmarshal([]byte(cleanedData), &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse local manifest: %w", err)
	}
	return manifest, nil
}

// BuildFromTree builds a manifest from a Git tree
func (m *Manager) BuildFromTree(ref string, tree []TreeItem, normalizePath func(string) string, getRawURL func(string, string) string) (map[string]FileInfo, error) {
	if !m.config.QuietFlag && m.config.VerboseFlag {
		fmt.Printf("Using ref: %s\n", ref)
	}

	// Convert tree to manifest format
	manifest := make(map[string]FileInfo)
	for _, item := range tree {
		// Only include files (blobs), not directories (trees)
		if item.Type != "blob" {
			continue
		}

		// Skip excluded files
		if m.ShouldExclude(item.Path, normalizePath) {
			continue
		}

		// Normalize path
		normalizedPath := normalizePath(item.Path)

		// Generate raw URL
		rawURL := getRawURL(ref, item.Path)

		manifest[normalizedPath] = FileInfo{
			Name: normalizedPath,
			Hash: item.SHA, // Git SHA-1 hash from GitHub API
			URL:  rawURL,
		}
	}

	if !m.config.QuietFlag && m.config.VerboseFlag {
		fmt.Printf("Found %d files in repository\n", len(manifest))
	}

	return manifest, nil
}

// ShouldExclude determines if a path should be excluded from the manifest
func (m *Manager) ShouldExclude(path string, normalizePath func(string) string) bool {
	// Normalize the path for case-insensitive comparison
	normalizedPath := strings.ToLower(normalizePath(path))

	excludeList := []string{
		".git/",
		".github/",
		".gitignore",
		".manifest",
		".updater-excludes",
		"worlds/plugin/state/",
		"update.exe",
		"updater.exe",
		"launcher.exe",
		"version.json",
		"mushclient_prefs.sqlite",
		"mushclient.ini",
	}

	for _, pattern := range excludeList {
		patternNormalized := strings.ToLower(pattern)
		if strings.HasPrefix(normalizedPath, patternNormalized) || normalizedPath == strings.TrimSuffix(patternNormalized, "/") {
			return true
		}
	}

	// Exclude .mcl files in worlds directory (user configuration files)
	if strings.HasPrefix(normalizedPath, m.config.WorldsDir+"/") && strings.HasSuffix(normalizedPath, m.config.WorldFileExt) {
		return true
	}

	return false
}

// Save saves a manifest to the local filesystem
func (m *Manager) Save(manifest map[string]FileInfo, denormalizePath func(string) string) error {
	baseDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Only save files to local manifest that exist both in remote AND locally on disk
	// This ensures the local manifest accurately represents what's actually installed
	localManifest := make(map[string]FileInfo)
	for path, info := range manifest {
		filePath := filepath.Join(baseDir, denormalizePath(path))
		if _, err := os.Stat(filePath); err == nil {
			// File exists locally, include it in the local manifest
			localManifest[path] = info
		}
	}

	// Save to local file
	data, err := json.MarshalIndent(localManifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := os.WriteFile(filepath.Join(baseDir, m.config.ManifestFile), append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("failed to save manifest: %w", err)
	}

	return nil
}
