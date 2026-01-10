package download

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cavaliergopher/grab/v3"
)

var client = grab.NewClient()

// ProgressCallback is called during download with progress info
type ProgressCallback func(bytesComplete, totalBytes int64, percentage int)

// File downloads a file from URL to the target path
func File(url, targetPath string) error {
	req, err := grab.NewRequest(targetPath, url)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.NoResume = true // Always overwrite, never resume

	resp := client.Do(req)
	if err := resp.Err(); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	return nil
}

// FileWithProgress downloads a file with progress callback
func FileWithProgress(url, targetPath string, callback ProgressCallback) error {
	req, err := grab.NewRequest(targetPath, url)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.NoResume = true // Always overwrite, never resume

	resp := client.Do(req)

	// Progress loop
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	lastPercentage := -1
	for {
		select {
		case <-ticker.C:
			if callback != nil {
				var percentage int
				if resp.Size() > 0 {
					percentage = int(resp.Progress() * 100)
				}
				if percentage != lastPercentage {
					callback(resp.BytesComplete(), resp.Size(), percentage)
					lastPercentage = percentage
				}
			}
		case <-resp.Done:
			if callback != nil && resp.Size() > 0 {
				callback(resp.BytesComplete(), resp.Size(), 100)
			}
			goto done
		}
	}
done:

	if err := resp.Err(); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	return nil
}

// ToTemp downloads a file to a temporary location and returns the path
func ToTemp(url, prefix string) (string, error) {
	tempFile, err := os.CreateTemp("", prefix+"*.tmp")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := File(url, tempPath); err != nil {
		_ = os.Remove(tempPath) // Best effort cleanup
		return "", err
	}

	return tempPath, nil
}

// ToTempWithProgress downloads with progress to a temp file
func ToTempWithProgress(url, prefix string, callback ProgressCallback) (string, error) {
	tempFile, err := os.CreateTemp("", prefix+"*.tmp")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := FileWithProgress(url, tempPath, callback); err != nil {
		_ = os.Remove(tempPath) // Best effort cleanup
		return "", err
	}

	return tempPath, nil
}

// ValidatePath ensures a path doesn't escape the base directory (path traversal protection)
func ValidatePath(basePath, targetPath string) (string, error) {
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve base path: %w", err)
	}

	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve target path: %w", err)
	}

	if !strings.HasPrefix(absTarget, absBase) {
		return "", fmt.Errorf("path traversal attempt detected")
	}

	return absTarget, nil
}
