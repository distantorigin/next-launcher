package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Config holds the configuration for self-update
type Config struct {
	VersionURL    string
	BinaryURL     string
	HashURL       string
	CurrentVersion string
}

// DefaultConfig returns the default self-update configuration
func DefaultConfig(currentVersion string) Config {
	return Config{
		VersionURL:    "https://anomalousabode.com/next/updater-version",
		BinaryURL:     "https://anomalousabode.com/next/updater",
		HashURL:       "https://anomalousabode.com/next/updater.sha256",
		CurrentVersion: currentVersion,
	}
}

// Check checks for a new version of the updater and replaces it if available.
// This function fails silently with a short timeout to avoid blocking the main update process.
// Returns true if the updater was replaced and a restart is needed.
func Check(cfg Config) error {
	// Get the path of the current executable
	exePath, err := os.Executable()
	if err != nil {
		return nil // Silent failure - not critical
	}

	// Create a client with a very short timeout for self-update check
	quickClient := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     10 * time.Second,
			DisableCompression:  false,
		},
	}

	// Make a request to the update URL
	resp, err := quickClient.Get(cfg.VersionURL)
	if err != nil {
		return nil // Silent failure - network issues, server down, etc.
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil // Silent failure - updater not available or other HTTP error
	}

	// Read the remote version
	versionData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	remoteVersion := strings.TrimSpace(string(versionData))
	if remoteVersion == "" || remoteVersion == cfg.CurrentVersion {
		return nil // No update available
	}

	// Update available - download and verify
	return downloadAndReplace(cfg, exePath)
}

// downloadAndReplace downloads the new binary, verifies it, and replaces the current executable
func downloadAndReplace(cfg Config, exePath string) error {
	downloadClient := &http.Client{Timeout: 30 * time.Second}

	// Download expected hash first
	hashResp, err := downloadClient.Get(cfg.HashURL)
	if err != nil {
		return nil // Silent failure - can't verify without hash
	}
	defer hashResp.Body.Close()

	if hashResp.StatusCode != http.StatusOK {
		return nil // Silent failure - hash not available
	}

	hashData, err := io.ReadAll(hashResp.Body)
	if err != nil {
		return nil
	}

	// Parse expected hash (format: "sha256hash  filename" or just "sha256hash")
	expectedHash := strings.TrimSpace(string(hashData))
	if idx := strings.Index(expectedHash, " "); idx > 0 {
		expectedHash = expectedHash[:idx]
	}
	expectedHash = strings.ToLower(expectedHash)

	if len(expectedHash) != 64 {
		return nil // Invalid hash format, refuse to update
	}

	// Download new binary
	binaryResp, err := downloadClient.Get(cfg.BinaryURL)
	if err != nil {
		return nil
	}
	defer binaryResp.Body.Close()

	if binaryResp.StatusCode != http.StatusOK {
		return nil
	}

	data, err := io.ReadAll(binaryResp.Body)
	if err != nil {
		return nil
	}

	// Verify SHA256 hash before replacing
	actualHash := sha256.Sum256(data)
	actualHashStr := hex.EncodeToString(actualHash[:])

	if actualHashStr != expectedHash {
		return nil // Hash mismatch - refuse to update
	}

	// Hash verified - safe to replace
	oldExe := exePath + ".old"
	_ = os.Remove(oldExe)
	if err := os.Rename(exePath, oldExe); err != nil {
		return nil
	}

	if err := os.WriteFile(exePath, data, 0755); err != nil {
		_ = os.Rename(oldExe, exePath)
		return nil
	}

	// Restart with same arguments
	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "UPDATER_CLEANUP_OLD=1")

	if err := cmd.Start(); err != nil {
		_ = os.Remove(exePath)
		_ = os.Rename(oldExe, exePath)
		return err
	}

	// Give the new process a moment to initialize before we exit
	time.Sleep(100 * time.Millisecond)
	os.Exit(0)

	return nil
}

// CleanupOld removes the .old backup file if UPDATER_CLEANUP_OLD env var is set
func CleanupOld() {
	if os.Getenv("UPDATER_CLEANUP_OLD") != "1" {
		return
	}

	exePath, err := os.Executable()
	if err != nil {
		return
	}

	oldExe := exePath + ".old"
	_ = os.Remove(oldExe)
}
