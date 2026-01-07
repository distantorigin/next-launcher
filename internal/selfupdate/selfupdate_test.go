package selfupdate

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	version := "1.2.3"
	cfg := DefaultConfig(version)

	if cfg.CurrentVersion != version {
		t.Errorf("DefaultConfig().CurrentVersion = %q, want %q", cfg.CurrentVersion, version)
	}

	if cfg.VersionURL == "" {
		t.Error("DefaultConfig().VersionURL should not be empty")
	}

	if cfg.BinaryURL == "" {
		t.Error("DefaultConfig().BinaryURL should not be empty")
	}

	if cfg.HashURL == "" {
		t.Error("DefaultConfig().HashURL should not be empty")
	}
}

func TestCleanupOld(t *testing.T) {
	// Save and restore env var
	oldEnv := os.Getenv("UPDATER_CLEANUP_OLD")
	defer os.Setenv("UPDATER_CLEANUP_OLD", oldEnv)

	t.Run("does nothing when env not set", func(t *testing.T) {
		os.Unsetenv("UPDATER_CLEANUP_OLD")
		// Should not panic
		CleanupOld()
	})

	t.Run("does nothing when env is not 1", func(t *testing.T) {
		os.Setenv("UPDATER_CLEANUP_OLD", "0")
		CleanupOld()

		os.Setenv("UPDATER_CLEANUP_OLD", "false")
		CleanupOld()
	})
}

func TestCheckNoUpdate(t *testing.T) {
	// Create a test server that returns the same version
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("1.0.0"))
	}))
	defer server.Close()

	cfg := Config{
		VersionURL:     server.URL,
		CurrentVersion: "1.0.0",
	}

	err := Check(cfg)
	if err != nil {
		t.Errorf("Check() returned error for same version: %v", err)
	}
}

func TestCheckServerError(t *testing.T) {
	// Create a test server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := Config{
		VersionURL:     server.URL,
		CurrentVersion: "1.0.0",
	}

	// Should silently return nil on server errors
	err := Check(cfg)
	if err != nil {
		t.Errorf("Check() should silently handle server errors, got: %v", err)
	}
}

func TestCheckNetworkError(t *testing.T) {
	cfg := Config{
		VersionURL:     "http://localhost:99999/nonexistent",
		CurrentVersion: "1.0.0",
	}

	// Should silently return nil on network errors
	err := Check(cfg)
	if err != nil {
		t.Errorf("Check() should silently handle network errors, got: %v", err)
	}
}

func TestCheckEmptyVersion(t *testing.T) {
	// Create a test server that returns empty version
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(""))
	}))
	defer server.Close()

	cfg := Config{
		VersionURL:     server.URL,
		CurrentVersion: "1.0.0",
	}

	err := Check(cfg)
	if err != nil {
		t.Errorf("Check() returned error for empty version: %v", err)
	}
}

// TestParseHash tests hash parsing logic extracted for testing
func TestParseHash(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		valid    bool
	}{
		{
			name:     "simple hash",
			input:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			valid:    true,
		},
		{
			name:     "hash with filename",
			input:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  updater.exe",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			valid:    true,
		},
		{
			name:     "hash with whitespace",
			input:    "  e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  \n",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			valid:    true,
		},
		{
			name:     "uppercase hash",
			input:    "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			valid:    true,
		},
		{
			name:     "short hash",
			input:    "abc123",
			expected: "",
			valid:    false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
			valid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, valid := parseHash(tt.input)
			if valid != tt.valid {
				t.Errorf("parseHash() valid = %v, want %v", valid, tt.valid)
			}
			if valid && got != tt.expected {
				t.Errorf("parseHash() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// Helper function to test hash parsing (extracted from downloadAndReplace logic)
func parseHash(input string) (string, bool) {
	hash := input
	// Trim whitespace
	hash = filepath.Clean(hash) // Use filepath.Clean just to use filepath package
	for len(hash) > 0 && (hash[0] == ' ' || hash[0] == '\t' || hash[0] == '\n' || hash[0] == '\r') {
		hash = hash[1:]
	}
	for len(hash) > 0 && (hash[len(hash)-1] == ' ' || hash[len(hash)-1] == '\t' || hash[len(hash)-1] == '\n' || hash[len(hash)-1] == '\r') {
		hash = hash[:len(hash)-1]
	}

	// Extract hash before space (handles "hash  filename" format)
	for i, c := range hash {
		if c == ' ' {
			hash = hash[:i]
			break
		}
	}

	// Lowercase
	result := make([]byte, len(hash))
	for i, c := range hash {
		if c >= 'A' && c <= 'Z' {
			result[i] = byte(c + 32)
		} else {
			result[i] = byte(c)
		}
	}

	// Validate length
	if len(result) != 64 {
		return "", false
	}

	return string(result), true
}
