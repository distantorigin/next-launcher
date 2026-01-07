package version

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionString(t *testing.T) {
	tests := []struct {
		name     string
		version  Version
		expected string
	}{
		{
			name:     "basic version",
			version:  Version{Major: 1, Minor: 2, Patch: 3},
			expected: "1.2.03",
		},
		{
			name:     "version with commit",
			version:  Version{Major: 1, Minor: 0, Patch: 0, Commit: "abc1234"},
			expected: "1.0.00+abc1234",
		},
		{
			name:     "zero version",
			version:  Version{},
			expected: "0.0.00",
		},
		{
			name:     "double digit patch",
			version:  Version{Major: 2, Minor: 5, Patch: 12},
			expected: "2.5.12",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.version.String()
			if got != tt.expected {
				t.Errorf("Version.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestParseTag(t *testing.T) {
	tests := []struct {
		name        string
		tag         string
		wantMajor   int
		wantMinor   int
		wantPatch   int
		wantErr     bool
	}{
		{
			name:      "valid tag with v prefix",
			tag:       "v1.2.3",
			wantMajor: 1,
			wantMinor: 2,
			wantPatch: 3,
		},
		{
			name:      "valid tag without v prefix",
			tag:       "1.2.3",
			wantMajor: 1,
			wantMinor: 2,
			wantPatch: 3,
		},
		{
			name:      "zero version",
			tag:       "v0.0.0",
			wantMajor: 0,
			wantMinor: 0,
			wantPatch: 0,
		},
		{
			name:      "large numbers",
			tag:       "v10.20.30",
			wantMajor: 10,
			wantMinor: 20,
			wantPatch: 30,
		},
		{
			name:    "missing patch",
			tag:     "v1.2",
			wantErr: true,
		},
		{
			name:    "too many parts",
			tag:     "v1.2.3.4",
			wantErr: true,
		},
		{
			name:    "non-numeric major",
			tag:     "vX.2.3",
			wantErr: true,
		},
		{
			name:    "non-numeric minor",
			tag:     "v1.Y.3",
			wantErr: true,
		},
		{
			name:    "non-numeric patch",
			tag:     "v1.2.Z",
			wantErr: true,
		},
		{
			name:    "empty string",
			tag:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			major, minor, patch, err := ParseTag(tt.tag)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTag() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if major != tt.wantMajor {
					t.Errorf("ParseTag() major = %d, want %d", major, tt.wantMajor)
				}
				if minor != tt.wantMinor {
					t.Errorf("ParseTag() minor = %d, want %d", minor, tt.wantMinor)
				}
				if patch != tt.wantPatch {
					t.Errorf("ParseTag() patch = %d, want %d", patch, tt.wantPatch)
				}
			}
		})
	}
}

func TestLoadLocalAndSave(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	versionFile := "version.json"

	// Test saving
	v := &Version{
		Major:  1,
		Minor:  2,
		Patch:  3,
		Commit: "abc1234",
		Date:   "2024-01-15",
	}

	err := Save(tmpDir, versionFile, v)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file exists
	path := filepath.Join(tmpDir, versionFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("Save() did not create file")
	}

	// Test loading
	loaded, err := LoadLocal(tmpDir, versionFile)
	if err != nil {
		t.Fatalf("LoadLocal() error = %v", err)
	}

	if loaded.Major != v.Major {
		t.Errorf("LoadLocal() Major = %d, want %d", loaded.Major, v.Major)
	}
	if loaded.Minor != v.Minor {
		t.Errorf("LoadLocal() Minor = %d, want %d", loaded.Minor, v.Minor)
	}
	if loaded.Patch != v.Patch {
		t.Errorf("LoadLocal() Patch = %d, want %d", loaded.Patch, v.Patch)
	}
	if loaded.Commit != v.Commit {
		t.Errorf("LoadLocal() Commit = %q, want %q", loaded.Commit, v.Commit)
	}
	if loaded.Date != v.Date {
		t.Errorf("LoadLocal() Date = %q, want %q", loaded.Date, v.Date)
	}
}

func TestLoadLocalErrors(t *testing.T) {
	tmpDir := t.TempDir()

	// Test loading non-existent file
	_, err := LoadLocal(tmpDir, "nonexistent.json")
	if err == nil {
		t.Error("LoadLocal() expected error for non-existent file")
	}

	// Test loading invalid JSON
	invalidPath := filepath.Join(tmpDir, "invalid.json")
	if err := os.WriteFile(invalidPath, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = LoadLocal(tmpDir, "invalid.json")
	if err == nil {
		t.Error("LoadLocal() expected error for invalid JSON")
	}
}
