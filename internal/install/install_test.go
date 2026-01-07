package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpdateWorldFile_Success tests successful world file update
func TestUpdateWorldFile_Success(t *testing.T) {
	tempDir := t.TempDir()
	worldFile := filepath.Join(tempDir, "test.mcl")

	// Create test world file with default server
	originalContent := `<?xml version="1.0" encoding="iso-8859-1"?>
<world>
  <name>Test World</name>
  <site="miriani.org"
  port="1234"
  use_proxy="y"
</world>`

	err := os.WriteFile(worldFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatalf("failed to create test world file: %v", err)
	}

	cfg := WorldFileConfig{
		DefaultServer: "miriani.org",
		LocalServer:   "localhost",
		ProxianiPort:  "1234",
		MUDMixerPort:  "5678",
	}

	// Update without port change
	err = UpdateWorldFile(worldFile, false, cfg)
	if err != nil {
		t.Fatalf("UpdateWorldFile() error = %v", err)
	}

	// Read updated content
	data, _ := os.ReadFile(worldFile)
	content := string(data)

	// Verify server was updated
	if !strings.Contains(content, `site="localhost"`) {
		t.Error("UpdateWorldFile() should update server to localhost")
	}

	// Verify port was NOT changed (updatePort=false)
	if !strings.Contains(content, `port="1234"`) {
		t.Error("UpdateWorldFile() should not change port when updatePort=false")
	}
}

// TestUpdateWorldFile_WithPortUpdate tests world file update with port change
func TestUpdateWorldFile_WithPortUpdate(t *testing.T) {
	tempDir := t.TempDir()
	worldFile := filepath.Join(tempDir, "test.mcl")

	originalContent := `<?xml version="1.0" encoding="iso-8859-1"?>
<world>
  <name>Test World</name>
  <site="miriani.org"
  port="1234"
</world>`

	err := os.WriteFile(worldFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatalf("failed to create test world file: %v", err)
	}

	cfg := WorldFileConfig{
		DefaultServer: "miriani.org",
		LocalServer:   "localhost",
		ProxianiPort:  "1234",
		MUDMixerPort:  "5678",
	}

	// Update WITH port change
	err = UpdateWorldFile(worldFile, true, cfg)
	if err != nil {
		t.Fatalf("UpdateWorldFile() error = %v", err)
	}

	// Read updated content
	data, _ := os.ReadFile(worldFile)
	content := string(data)

	// Verify both server and port were updated
	if !strings.Contains(content, `site="localhost"`) {
		t.Error("UpdateWorldFile() should update server to localhost")
	}

	if !strings.Contains(content, `port="5678"`) {
		t.Error("UpdateWorldFile() should update port when updatePort=true")
	}

	if strings.Contains(content, `port="1234"`) {
		t.Error("UpdateWorldFile() should replace old port")
	}
}

// TestUpdateWorldFile_NoServerFound tests error when server not found
func TestUpdateWorldFile_NoServerFound(t *testing.T) {
	tempDir := t.TempDir()
	worldFile := filepath.Join(tempDir, "test.mcl")

	// Create world file without the target server
	originalContent := `<?xml version="1.0" encoding="iso-8859-1"?>
<world>
  <name>Test World</name>
  <site="different-server.com"
  port="1234"
</world>`

	err := os.WriteFile(worldFile, []byte(originalContent), 0644)
	if err != nil {
		t.Fatalf("failed to create test world file: %v", err)
	}

	cfg := WorldFileConfig{
		DefaultServer: "miriani.org",
		LocalServer:   "localhost",
		ProxianiPort:  "1234",
		MUDMixerPort:  "5678",
	}

	err = UpdateWorldFile(worldFile, false, cfg)
	if err == nil {
		t.Error("UpdateWorldFile() expected error when server not found, got nil")
	}

	if !strings.Contains(err.Error(), "no miriani.org references found") {
		t.Errorf("UpdateWorldFile() error = %v, want error about server not found", err)
	}
}

// TestUpdateWorldFile_MissingFile tests error handling for missing file
func TestUpdateWorldFile_MissingFile(t *testing.T) {
	cfg := WorldFileConfig{
		DefaultServer: "miriani.org",
		LocalServer:   "localhost",
		ProxianiPort:  "1234",
		MUDMixerPort:  "5678",
	}

	err := UpdateWorldFile("/nonexistent/file.mcl", false, cfg)
	if err == nil {
		t.Error("UpdateWorldFile() expected error for missing file, got nil")
	}

	if !strings.Contains(err.Error(), "failed to read") {
		t.Errorf("UpdateWorldFile() error = %v, want error about reading file", err)
	}
}

// TestCreateChannelSwitchBatchFiles tests batch file creation
func TestCreateChannelSwitchBatchFiles(t *testing.T) {
	tempDir := t.TempDir()

	err := CreateChannelSwitchBatchFiles(tempDir)
	if err != nil {
		t.Fatalf("CreateChannelSwitchBatchFiles() error = %v", err)
	}

	expectedFiles := map[string]string{
		"Switch to Stable.bat":      "update.exe switch stable",
		"Switch to Dev.bat":         "update.exe switch dev",
		"Switch to Any Channel.bat": "update.exe switch",
	}

	for filename, expectedContent := range expectedFiles {
		path := filepath.Join(tempDir, filename)

		// Check file exists
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("CreateChannelSwitchBatchFiles() should create %s", filename)
			continue
		}

		// Check file content
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("failed to read %s: %v", filename, err)
			continue
		}

		content := string(data)
		if !strings.Contains(content, expectedContent) {
			t.Errorf("%s content = %q, want to contain %q", filename, content, expectedContent)
		}

		// Verify it's a batch file (starts with @echo off)
		if !strings.HasPrefix(content, "@echo off") {
			t.Errorf("%s should start with @echo off", filename)
		}
	}
}

// TestCreateChannelSwitchBatchFiles_InvalidDir tests error handling
func TestCreateChannelSwitchBatchFiles_InvalidDir(t *testing.T) {
	err := CreateChannelSwitchBatchFiles("/nonexistent/directory/that/does/not/exist")
	if err == nil {
		t.Error("CreateChannelSwitchBatchFiles() expected error for invalid directory, got nil")
	}
}

// TestIsInstalled tests installation detection
func TestIsInstalled(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(string) error
		want          bool
		description   string
	}{
		{
			name: "valid installation with MUSHclient and worlds",
			setupFunc: func(dir string) error {
				os.WriteFile(filepath.Join(dir, "MUSHclient.exe"), []byte("fake"), 0644)
				os.Mkdir(filepath.Join(dir, "worlds"), 0755)
				return nil
			},
			want:        true,
			description: "should detect MUSHclient.exe + worlds directory",
		},
		{
			name: "valid installation with mushclient (lowercase) and worlds",
			setupFunc: func(dir string) error {
				os.WriteFile(filepath.Join(dir, "mushclient.exe"), []byte("fake"), 0644)
				os.Mkdir(filepath.Join(dir, "worlds"), 0755)
				return nil
			},
			want:        true,
			description: "should detect lowercase mushclient.exe",
		},
		{
			name: "valid installation with MUSHclient and manifest",
			setupFunc: func(dir string) error {
				os.WriteFile(filepath.Join(dir, "MUSHclient.exe"), []byte("fake"), 0644)
				os.WriteFile(filepath.Join(dir, ".manifest"), []byte("{}"), 0644)
				return nil
			},
			want:        true,
			description: "should detect MUSHclient.exe + .manifest",
		},
		{
			name: "missing MUSHclient",
			setupFunc: func(dir string) error {
				os.Mkdir(filepath.Join(dir, "worlds"), 0755)
				return nil
			},
			want:        false,
			description: "should return false without MUSHclient.exe",
		},
		{
			name: "MUSHclient only (no worlds or manifest)",
			setupFunc: func(dir string) error {
				os.WriteFile(filepath.Join(dir, "MUSHclient.exe"), []byte("fake"), 0644)
				return nil
			},
			want:        false,
			description: "should return false without worlds or manifest",
		},
		{
			name: "empty directory",
			setupFunc: func(dir string) error {
				return nil
			},
			want:        false,
			description: "should return false for empty directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()

			if err := tt.setupFunc(tempDir); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			got := IsInstalled(tempDir)
			if got != tt.want {
				t.Errorf("IsInstalled() = %v, want %v (%s)", got, tt.want, tt.description)
			}
		})
	}
}

// TestHasWorldFiles tests world file detection
func TestHasWorldFiles(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(string) error
		want      bool
	}{
		{
			name: "MUSHclient.exe exists (shortcut check)",
			setupFunc: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "MUSHclient.exe"), []byte("fake"), 0644)
			},
			want: true,
		},
		{
			name: "worlds directory with .mcl files",
			setupFunc: func(dir string) error {
				worldsDir := filepath.Join(dir, "worlds")
				os.Mkdir(worldsDir, 0755)
				return os.WriteFile(filepath.Join(worldsDir, "game.mcl"), []byte("content"), 0644)
			},
			want: true,
		},
		{
			name: "worlds directory with .MCL files (uppercase)",
			setupFunc: func(dir string) error {
				worldsDir := filepath.Join(dir, "worlds")
				os.Mkdir(worldsDir, 0755)
				return os.WriteFile(filepath.Join(worldsDir, "game.MCL"), []byte("content"), 0644)
			},
			want: true,
		},
		{
			name: "worlds directory without .mcl files",
			setupFunc: func(dir string) error {
				worldsDir := filepath.Join(dir, "worlds")
				os.Mkdir(worldsDir, 0755)
				os.WriteFile(filepath.Join(worldsDir, "readme.txt"), []byte("content"), 0644)
				return nil
			},
			want: false,
		},
		{
			name: "no worlds directory",
			setupFunc: func(dir string) error {
				return nil
			},
			want: false,
		},
		{
			name: "worlds is a file, not directory",
			setupFunc: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "worlds"), []byte("not a dir"), 0644)
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()

			if err := tt.setupFunc(tempDir); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			got := HasWorldFiles(tempDir)
			if got != tt.want {
				t.Errorf("HasWorldFiles() = %v, want %v", got, tt.want)
			}
		})
	}
}
