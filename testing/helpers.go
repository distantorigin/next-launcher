package testing

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TempDir creates a temporary directory for testing and registers cleanup
func TempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "updater-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(dir)
	})
	return dir
}

// WriteFile creates a test file with content
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	err = os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
}

// CopyFile copies a file for testing
func CopyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("failed to open source: %v", err)
	}
	defer in.Close()

	err = os.MkdirAll(filepath.Dir(dst), 0755)
	if err != nil {
		t.Fatalf("failed to create destination directory: %v", err)
	}

	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("failed to create destination: %v", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("failed to copy: %v", err)
	}
}

// AssertFileExists checks if a file exists
func AssertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("file does not exist: %s", path)
	}
}

// AssertFileNotExists checks if a file does not exist
func AssertFileNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should not exist: %s", path)
	}
}

// AssertFileContent checks file content matches expected
func AssertFileContent(t *testing.T, path, expected string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file %s: %v", path, err)
	}
	if string(content) != expected {
		t.Errorf("file content mismatch for %s:\nwant: %q\ngot:  %q", path, expected, string(content))
	}
}

// AssertError checks that an error occurred
func AssertError(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: expected error, got nil", msg)
	}
}

// AssertNoError checks that no error occurred
func AssertNoError(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: unexpected error: %v", msg, err)
	}
}

// AssertEqual checks if two values are equal
func AssertEqual(t *testing.T, got, want interface{}, msg string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", msg, got, want)
	}
}

// AssertContains checks if a string contains a substring
func AssertContains(t *testing.T, s, substr, msg string) {
	t.Helper()
	if len(s) < len(substr) {
		t.Errorf("%s: string too short to contain substring", msg)
		return
	}
	found := false
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("%s: string %q does not contain %q", msg, s, substr)
	}
}
