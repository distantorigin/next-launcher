package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadLocal_WithComments tests loading manifest with // comments
func TestLoadLocal_WithComments(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, ".manifest")

	// Create manifest with comments
	content := `{
  // This is a comment
  "file1.txt": {
    "name": "file1.txt",
    "hash": "abc123",
    "url": "https://example.com/file1.txt"
  },
  // Another comment
  "file2.txt": {
    "name": "file2.txt",
    "hash": "def456",
    "url": "https://example.com/file2.txt"
  }
  // Final comment
}`

	err := os.WriteFile(manifestPath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to write test manifest: %v", err)
	}

	// Change to temp directory
	originalDir, _ := os.Getwd()
	os.Chdir(tempDir)
	defer os.Chdir(originalDir)

	manager := NewManager(Config{
		ManifestFile: ".manifest",
	})

	manifest, err := manager.LoadLocal()
	if err != nil {
		t.Fatalf("LoadLocal() error = %v", err)
	}

	if len(manifest) != 2 {
		t.Errorf("LoadLocal() returned %d files, want 2", len(manifest))
	}

	if _, exists := manifest["file1.txt"]; !exists {
		t.Error("LoadLocal() missing file1.txt")
	}

	if _, exists := manifest["file2.txt"]; !exists {
		t.Error("LoadLocal() missing file2.txt")
	}

	// Verify file details
	if manifest["file1.txt"].Hash != "abc123" {
		t.Errorf("file1.txt hash = %s, want abc123", manifest["file1.txt"].Hash)
	}
}

// TestLoadLocal_InvalidJSON tests error handling for corrupt manifest
func TestLoadLocal_InvalidJSON(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, ".manifest")

	// Create invalid JSON
	content := `{
  "file1.txt": {
    "name": "file1.txt",
    "hash": "abc123"
    // Missing closing braces
`

	err := os.WriteFile(manifestPath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to write test manifest: %v", err)
	}

	originalDir, _ := os.Getwd()
	os.Chdir(tempDir)
	defer os.Chdir(originalDir)

	manager := NewManager(Config{
		ManifestFile: ".manifest",
	})

	_, err = manager.LoadLocal()
	if err == nil {
		t.Error("LoadLocal() expected error for invalid JSON, got nil")
	}

	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("LoadLocal() error = %v, want error containing 'parse'", err)
	}
}

// TestLoadLocal_MissingFile tests error when manifest doesn't exist
func TestLoadLocal_MissingFile(t *testing.T) {
	tempDir := t.TempDir()

	originalDir, _ := os.Getwd()
	os.Chdir(tempDir)
	defer os.Chdir(originalDir)

	manager := NewManager(Config{
		ManifestFile: ".manifest",
	})

	_, err := manager.LoadLocal()
	if err == nil {
		t.Error("LoadLocal() expected error for missing file, got nil")
	}
}

// TestShouldExclude tests file exclusion logic
func TestShouldExclude(t *testing.T) {
	manager := NewManager(Config{
		WorldsDir:    "worlds",
		WorldFileExt: ".mcl",
	})

	// Mock normalization function
	normalize := func(p string) string {
		return strings.ReplaceAll(p, "\\", "/")
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		// Should exclude
		{name: "git directory", path: ".git/config", want: true},
		{name: "github directory", path: ".github/workflows/test.yml", want: true},
		{name: "gitignore", path: ".gitignore", want: true},
		{name: "manifest file", path: ".manifest", want: true},
		{name: "excludes file", path: ".updater-excludes", want: true},
		{name: "updater exe", path: "updater.exe", want: true},
		{name: "update exe", path: "update.exe", want: true},
		{name: "launcher exe", path: "launcher.exe", want: true},
		{name: "version json", path: "version.json", want: true},
		{name: "mushclient prefs", path: "mushclient_prefs.sqlite", want: true},
		{name: "mushclient ini", path: "mushclient.ini", want: true},
		{name: "world file", path: "worlds/miriani.mcl", want: true},
		{name: "world file nested", path: "worlds/custom/game.mcl", want: true},
		{name: "plugin state", path: "worlds/plugin/state/data.json", want: true},

		// Should NOT exclude
		{name: "readme", path: "README.md", want: false},
		{name: "source file", path: "src/main.go", want: false},
		{name: "plugin file", path: "worlds/plugins/myplugin.xml", want: false},
		{name: "worlds directory non-mcl", path: "worlds/readme.txt", want: false},
		{name: "mcl outside worlds", path: "backup/world.mcl", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.ShouldExclude(tt.path, normalize)
			if got != tt.want {
				t.Errorf("ShouldExclude(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestShouldExclude_CaseInsensitive tests that exclusion is case-insensitive
func TestShouldExclude_CaseInsensitive(t *testing.T) {
	manager := NewManager(Config{
		WorldsDir:    "worlds",
		WorldFileExt: ".mcl",
	})

	normalize := func(p string) string {
		return strings.ReplaceAll(p, "\\", "/")
	}

	tests := []string{
		".GIT/config",
		".GITHUB/workflows/test.yml",
		"UPDATER.EXE",
		"MushClient_Prefs.SQLITE",
		"WORLDS/Miriani.MCL",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			if !manager.ShouldExclude(path, normalize) {
				t.Errorf("ShouldExclude(%q) = false, want true (case-insensitive)", path)
			}
		})
	}
}

// TestBuildFromTree tests building manifest from tree items
func TestBuildFromTree(t *testing.T) {
	manager := NewManager(Config{
		ManifestFile: ".manifest",
		WorldsDir:    "worlds",
		WorldFileExt: ".mcl",
		QuietFlag:    true, // Suppress output during test
	})

	normalize := func(p string) string {
		return strings.ReplaceAll(p, "\\", "/")
	}

	getRawURL := func(ref, path string) string {
		return "https://raw.githubusercontent.com/owner/repo/" + ref + "/" + path
	}

	tree := []TreeItem{
		{Path: "README.md", Type: "blob", SHA: "abc123"},
		{Path: "src/main.go", Type: "blob", SHA: "def456"},
		{Path: ".git/config", Type: "blob", SHA: "should-exclude"},
		{Path: "updater.exe", Type: "blob", SHA: "should-exclude-exe"},
		{Path: "worlds/miriani.mcl", Type: "blob", SHA: "should-exclude-mcl"},
		{Path: "worlds/plugins/plugin.xml", Type: "blob", SHA: "ghi789"},
		{Path: "src", Type: "tree", SHA: "tree-sha"}, // directory, should skip
	}

	manifest, err := manager.BuildFromTree("main", tree, normalize, getRawURL)
	if err != nil {
		t.Fatalf("BuildFromTree() error = %v", err)
	}

	// Should have 3 files: README.md, src/main.go, worlds/plugins/plugin.xml
	if len(manifest) != 3 {
		t.Errorf("BuildFromTree() returned %d files, want 3", len(manifest))
	}

	// Verify included files
	expectedFiles := []string{"README.md", "src/main.go", "worlds/plugins/plugin.xml"}
	for _, file := range expectedFiles {
		if _, exists := manifest[file]; !exists {
			t.Errorf("BuildFromTree() missing expected file: %s", file)
		}
	}

	// Verify excluded files are not present
	excludedFiles := []string{".git/config", "updater.exe", "worlds/miriani.mcl"}
	for _, file := range excludedFiles {
		if _, exists := manifest[file]; exists {
			t.Errorf("BuildFromTree() should exclude file: %s", file)
		}
	}

	// Verify file info is correct
	if manifest["README.md"].Hash != "abc123" {
		t.Errorf("README.md hash = %s, want abc123", manifest["README.md"].Hash)
	}

	if manifest["README.md"].URL != "https://raw.githubusercontent.com/owner/repo/main/README.md" {
		t.Errorf("README.md URL = %s, want correct raw URL", manifest["README.md"].URL)
	}
}

// TestSave tests saving manifest to file
func TestSave(t *testing.T) {
	tempDir := t.TempDir()

	// Create some test files
	os.WriteFile(filepath.Join(tempDir, "file1.txt"), []byte("content1"), 0644)
	os.WriteFile(filepath.Join(tempDir, "file2.txt"), []byte("content2"), 0644)

	originalDir, _ := os.Getwd()
	os.Chdir(tempDir)
	defer os.Chdir(originalDir)

	manager := NewManager(Config{
		ManifestFile: ".manifest",
	})

	denormalize := func(p string) string {
		return strings.ReplaceAll(p, "/", string(filepath.Separator))
	}

	// Create manifest with 3 files, but only 2 exist on disk
	manifest := map[string]FileInfo{
		"file1.txt": {
			Name: "file1.txt",
			Hash: "abc123",
			URL:  "https://example.com/file1.txt",
		},
		"file2.txt": {
			Name: "file2.txt",
			Hash: "def456",
			URL:  "https://example.com/file2.txt",
		},
		"file3.txt": {
			Name: "file3.txt",
			Hash: "ghi789",
			URL:  "https://example.com/file3.txt",
		},
	}

	err := manager.Save(manifest, denormalize)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Read back the manifest
	data, err := os.ReadFile(filepath.Join(tempDir, ".manifest"))
	if err != nil {
		t.Fatalf("failed to read saved manifest: %v", err)
	}

	var savedManifest map[string]FileInfo
	if err := json.Unmarshal(data, &savedManifest); err != nil {
		t.Fatalf("failed to parse saved manifest: %v", err)
	}

	// Should only have 2 files (file3.txt doesn't exist on disk)
	if len(savedManifest) != 2 {
		t.Errorf("Save() saved %d files, want 2 (only files that exist)", len(savedManifest))
	}

	if _, exists := savedManifest["file1.txt"]; !exists {
		t.Error("Save() missing file1.txt")
	}

	if _, exists := savedManifest["file2.txt"]; !exists {
		t.Error("Save() missing file2.txt")
	}

	if _, exists := savedManifest["file3.txt"]; exists {
		t.Error("Save() should not include file3.txt (doesn't exist on disk)")
	}
}

// TestSave_EmptyManifest tests saving empty manifest
func TestSave_EmptyManifest(t *testing.T) {
	tempDir := t.TempDir()

	originalDir, _ := os.Getwd()
	os.Chdir(tempDir)
	defer os.Chdir(originalDir)

	manager := NewManager(Config{
		ManifestFile: ".manifest",
	})

	denormalize := func(p string) string {
		return strings.ReplaceAll(p, "/", string(filepath.Separator))
	}

	manifest := map[string]FileInfo{}

	err := manager.Save(manifest, denormalize)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify empty manifest was saved
	data, err := os.ReadFile(filepath.Join(tempDir, ".manifest"))
	if err != nil {
		t.Fatalf("failed to read saved manifest: %v", err)
	}

	var savedManifest map[string]FileInfo
	if err := json.Unmarshal(data, &savedManifest); err != nil {
		t.Fatalf("failed to parse saved manifest: %v", err)
	}

	if len(savedManifest) != 0 {
		t.Errorf("Save() saved %d files, want 0", len(savedManifest))
	}
}

// TestNewManager tests manager creation
func TestNewManager(t *testing.T) {
	config := Config{
		ManifestFile: ".manifest",
		WorldsDir:    "worlds",
		WorldFileExt: ".mcl",
	}

	manager := NewManager(config)

	if manager == nil {
		t.Fatal("NewManager() returned nil")
	}

	if manager.config.ManifestFile != ".manifest" {
		t.Errorf("NewManager() config.ManifestFile = %s, want .manifest", manager.config.ManifestFile)
	}
}
