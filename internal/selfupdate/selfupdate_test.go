package selfupdate

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	version := "1.2.3"
	cfg := DefaultConfig(version)

	if cfg.CurrentVersion != version {
		t.Errorf("DefaultConfig().CurrentVersion = %q, want %q", cfg.CurrentVersion, version)
	}

	if cfg.ReleasesAPIURL == "" {
		t.Error("DefaultConfig().ReleasesAPIURL should not be empty")
	}

	if cfg.BinaryURL == "" {
		t.Error("DefaultConfig().BinaryURL should not be empty")
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
	// Create a test server that returns the same version via GitHub API format
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name": "v1.0.0", "assets": []}`))
	}))
	defer server.Close()

	cfg := Config{
		ReleasesAPIURL: server.URL,
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
		ReleasesAPIURL: server.URL,
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
		ReleasesAPIURL: "http://localhost:99999/nonexistent",
		CurrentVersion: "1.0.0",
	}

	// Should silently return nil on network errors
	err := Check(cfg)
	if err != nil {
		t.Errorf("Check() should silently handle network errors, got: %v", err)
	}
}

func TestCheckEmptyVersion(t *testing.T) {
	// Create a test server that returns empty tag
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name": "", "assets": []}`))
	}))
	defer server.Close()

	cfg := Config{
		ReleasesAPIURL: server.URL,
		CurrentVersion: "1.0.0",
	}

	err := Check(cfg)
	if err != nil {
		t.Errorf("Check() returned error for empty version: %v", err)
	}
}

func TestCheckInvalidJSON(t *testing.T) {
	// Create a test server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not valid json`))
	}))
	defer server.Close()

	cfg := Config{
		ReleasesAPIURL: server.URL,
		CurrentVersion: "1.0.0",
	}

	// Should silently return nil on parse errors
	err := Check(cfg)
	if err != nil {
		t.Errorf("Check() should silently handle invalid JSON, got: %v", err)
	}
}
