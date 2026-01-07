package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNormalize tests path normalization (backslash to forward slash)
func TestNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "Windows backslash to forward slash",
			input: "C:\\Users\\test\\file.txt",
			want:  "C:/Users/test/file.txt",
		},
		{
			name:  "already normalized",
			input: "C:/Users/test/file.txt",
			want:  "C:/Users/test/file.txt",
		},
		{
			name:  "mixed separators",
			input: "C:\\Users/test\\file.txt",
			want:  "C:/Users/test/file.txt",
		},
		{
			name:  "relative path",
			input: "..\\sub\\file.txt",
			want:  "../sub/file.txt",
		},
		{
			name:  "empty string becomes dot (filepath.Clean behavior)",
			input: "",
			want:  ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Normalize(tt.input)
			if got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestDenormalize tests path denormalization (forward slash to OS separator)
func TestDenormalize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "forward slash to backslash on Windows",
			input: "C:/Users/test/file.txt",
			want:  "C:" + string(filepath.Separator) + "Users" + string(filepath.Separator) + "test" + string(filepath.Separator) + "file.txt",
		},
		{
			name:  "already denormalized",
			input: "C:\\Users\\test\\file.txt",
			want:  "C:\\Users\\test\\file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Denormalize(tt.input)
			if got != tt.want {
				t.Errorf("Denormalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestIsUserConfig tests user configuration file detection
func TestIsUserConfig(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "mushclient preferences",
			path: "mushclient_prefs.sqlite",
			want: true,
		},
		{
			name: "mushclient ini",
			path: "mushclient.ini",
			want: true,
		},
		{
			name: "world file in worlds directory",
			path: "worlds/miriani.mcl",
			want: true,
		},
		{
			name: "world file nested",
			path: "worlds/subfolder/custom.mcl",
			want: true,
		},
		{
			name: "plugin state",
			path: "worlds/plugins/state/data.json",
			want: true,
		},
		{
			name: "log file",
			path: "logs/2024-01-05.log",
			want: true,
		},
		{
			name: "log file nested",
			path: "logs/2024/01/debug.log",
			want: true,
		},
		{
			name: "settings file",
			path: "worlds/settings/config.xml",
			want: true,
		},
		{
			name: "regular file",
			path: "updater.exe",
			want: false,
		},
		{
			name: "source code",
			path: "src/main.go",
			want: false,
		},
		{
			name: "readme",
			path: "README.md",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsUserConfig(tt.path)
			if got != tt.want {
				t.Errorf("IsUserConfig(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestMatchesExclusion tests exclusion pattern matching
func TestMatchesExclusion(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		patterns []string
		want     bool
	}{
		{
			name:     "exact match",
			path:     "file.txt",
			patterns: []string{"file.txt"},
			want:     true,
		},
		{
			name:     "wildcard extension",
			path:     "test.log",
			patterns: []string{"*.log"},
			want:     true,
		},
		{
			name:     "directory prefix lowercase",
			path:     "logs/debug.log",
			patterns: []string{"logs/"},
			want:     true,
		},
		{
			name:     "no match",
			path:     "src/main.go",
			patterns: []string{"*.txt"},
			want:     false,
		},
		{
			name:     "wildcard in middle",
			path:     "test_backup.txt",
			patterns: []string{"test_*.txt"},
			want:     true,
		},
		{
			name:     "multiple patterns - first matches",
			path:     "test.log",
			patterns: []string{"*.log", "*.txt"},
			want:     true,
		},
		{
			name:     "multiple patterns - second matches",
			path:     "test.txt",
			patterns: []string{"*.log", "*.txt"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create excludes map from patterns
			excludes := make(map[string]struct{})
			for _, pattern := range tt.patterns {
				// Preserve trailing slash for directory patterns
				// (Normalize() would remove it via filepath.Clean)
				if strings.HasSuffix(pattern, "/") {
					normalized := strings.ToLower(strings.ReplaceAll(pattern, "\\", "/"))
					excludes[normalized] = struct{}{}
				} else {
					excludes[strings.ToLower(Normalize(pattern))] = struct{}{}
				}
			}

			got := MatchesExclusion(tt.path, excludes)
			if got != tt.want {
				t.Errorf("MatchesExclusion(%q, %v) = %v, want %v", tt.path, tt.patterns, got, tt.want)
			}
		})
	}
}

// TestLoadExcludes tests loading exclusion patterns from file
func TestLoadExcludes(t *testing.T) {
	tempDir := t.TempDir()
	excludeFile := filepath.Join(tempDir, ".updater-excludes")

	content := `# Comment line
*.log
temp/
# Another comment

*.bak
# Empty lines and whitespace should be ignored

`

	err := os.WriteFile(excludeFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	excludes := LoadExcludes(excludeFile)

	// Expected patterns (normalized and lowercased)
	expected := []string{"*.log", "temp/", "*.bak"}

	if len(excludes) != len(expected) {
		t.Fatalf("LoadExcludes() returned %d patterns, want %d", len(excludes), len(expected))
	}

	// Check each expected pattern exists in the map
	for _, pattern := range expected {
		normalized := strings.ToLower(Normalize(pattern))
		if _, exists := excludes[normalized]; !exists {
			t.Errorf("LoadExcludes() missing pattern %q", pattern)
		}
	}
}

// TestLoadExcludes_FileNotFound tests graceful handling when file doesn't exist
func TestLoadExcludes_FileNotFound(t *testing.T) {
	excludes := LoadExcludes("/nonexistent/path/.updater-excludes")

	// Should return empty map (no error returned)
	if len(excludes) != 0 {
		t.Errorf("LoadExcludes() returned %d patterns for nonexistent file, want 0", len(excludes))
	}
}

// TestFindActual tests case-insensitive file lookup
func TestFindActual(t *testing.T) {
	tempDir := t.TempDir()

	// Create a file with specific casing
	testFile := filepath.Join(tempDir, "TestFile.txt")
	err := os.WriteFile(testFile, []byte("content"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Test exact case match
	t.Run("exact case match", func(t *testing.T) {
		got, err := FindActual(filepath.Join(tempDir, "TestFile.txt"))
		if err != nil {
			t.Errorf("FindActual() unexpected error: %v", err)
		}

		gotFile := filepath.Base(got)
		if gotFile != "TestFile.txt" {
			t.Errorf("FindActual() = %q, want %q", gotFile, "TestFile.txt")
		}
	})

	// Test file not found
	t.Run("file not found returns original", func(t *testing.T) {
		got, err := FindActual(filepath.Join(tempDir, "nonexistent.txt"))
		if err != nil {
			t.Errorf("FindActual() unexpected error: %v", err)
		}

		gotFile := filepath.Base(got)
		if gotFile != "nonexistent.txt" {
			t.Errorf("FindActual() = %q, want %q", gotFile, "nonexistent.txt")
		}
	})

	// Case-insensitive lookup is filesystem-dependent, skip for portability
	t.Log("Skipping case-insensitive tests - behavior depends on filesystem")
}
