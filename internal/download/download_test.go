package download

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidatePath_PreventTraversal tests path traversal protection (SECURITY CRITICAL)
func TestValidatePath_PreventTraversal(t *testing.T) {
	tests := []struct {
		name      string
		basePath  string
		target    string
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "valid path within base",
			basePath: "C:\\base",
			target:   "C:\\base\\file.txt",
			wantErr:  false,
		},
		{
			name:     "valid subdirectory",
			basePath: "C:\\base",
			target:   "C:\\base\\sub\\file.txt",
			wantErr:  false,
		},
		{
			name:      "relative path traversal with ..",
			basePath:  "C:\\base",
			target:    "C:\\base\\..\\etc\\passwd",
			wantErr:   true,
			errSubstr: "traversal",
		},
		{
			name:      "absolute path outside base",
			basePath:  "C:\\base",
			target:    "C:\\other\\file.txt",
			wantErr:   true,
			errSubstr: "traversal",
		},
		{
			name:      "multiple .. attempts",
			basePath:  "C:\\base\\sub",
			target:    "C:\\base\\sub\\..\\..\\..\\Windows\\System32",
			wantErr:   true,
			errSubstr: "traversal",
		},
		{
			name:      "forward slash variation",
			basePath:  "C:\\base",
			target:    "C:/base/../../etc/passwd",
			wantErr:   true,
			errSubstr: "traversal",
		},
		{
			name:      "deep nesting then escape",
			basePath:  "C:\\base",
			target:    "C:\\base\\a\\b\\c\\..\\..\\..\\..\\Windows",
			wantErr:   true,
			errSubstr: "traversal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidatePath(tt.basePath, tt.target)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidatePath() expected error, got nil")
					return
				}
				if tt.errSubstr != "" && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.errSubstr)) {
					t.Errorf("ValidatePath() error = %v, want substring %q", err, tt.errSubstr)
				}
			} else {
				if err != nil {
					t.Errorf("ValidatePath() unexpected error: %v", err)
					return
				}
				if result == "" {
					t.Errorf("ValidatePath() returned empty path")
				}
			}
		})
	}
}

// TestValidatePath_WithTempDirs tests with real filesystem paths
func TestValidatePath_WithTempDirs(t *testing.T) {
	tempBase := t.TempDir()

	// Create a subdirectory
	subDir := filepath.Join(tempBase, "sub")
	err := os.MkdirAll(subDir, 0755)
	if err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{
			name:    "file in base",
			target:  filepath.Join(tempBase, "file.txt"),
			wantErr: false,
		},
		{
			name:    "file in subdirectory",
			target:  filepath.Join(subDir, "file.txt"),
			wantErr: false,
		},
		{
			name:    "attempt to escape via ..",
			target:  filepath.Join(tempBase, "..", "outside.txt"),
			wantErr: true,
		},
		{
			name:    "attempt to use temp root",
			target:  filepath.Join(os.TempDir(), "outside.txt"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidatePath(tempBase, tt.target)

			if tt.wantErr && err == nil {
				t.Errorf("ValidatePath() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidatePath() unexpected error: %v", err)
			}
		})
	}
}

// TestToTemp tests temporary file download
func TestToTemp(t *testing.T) {
	// This test would require a mock HTTP server
	// Skip for now - implement after mock server is available
	t.Skip("requires HTTP mock server - see testing/mocks.go")
}

// TestFileWithProgress tests download with progress callback
func TestFileWithProgress(t *testing.T) {
	// This test would require a mock HTTP server
	// Skip for now - implement after mock server is available
	t.Skip("requires HTTP mock server - see testing/mocks.go")
}

// TestFile_CleanupOnError tests that files are cleaned up on error
func TestFile_CleanupOnError(t *testing.T) {
	// This test would require a mock HTTP server
	// Skip for now - implement after mock server is available
	t.Skip("requires HTTP mock server - see testing/mocks.go")
}

// Note: Add more tests for File(), FileWithProgress(), ToTemp(), ToTempWithProgress()
// once HTTP mock server infrastructure is in place
