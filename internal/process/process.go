package process

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// IsNodeListeningOnPort checks if node.exe is running and listening on the specified port
func IsNodeListeningOnPort(port string) bool {
	// Check if node.exe is running
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq node.exe", "/FO", "CSV", "/NH")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// If no node.exe processes, not running
	if !strings.Contains(string(output), "node.exe") {
		return false
	}

	// Check if port is in use
	return IsPortListening(port)
}

// IsPortListening checks if a TCP port is in LISTENING state
func IsPortListening(port string) bool {
	cmd := exec.Command("netstat", "-ano", "-p", "tcp")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, ":"+port) && strings.Contains(line, "LISTENING") {
			return true
		}
	}

	return false
}

// IsMUSHClientRunningInDir checks if MUSHclient.exe is running from the specified directory
func IsMUSHClientRunningInDir(targetDir string) bool {
	expectedPath := filepath.Join(targetDir, "MUSHclient.exe")
	expectedPath = strings.ToLower(filepath.Clean(expectedPath))

	// Use WMIC to get all running MUSHclient.exe processes with their full paths
	cmd := exec.Command("wmic", "process", "where", "name='MUSHclient.exe'", "get", "ExecutablePath", "/format:list")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// Parse output - format is "ExecutablePath=C:\path\to\MUSHclient.exe"
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ExecutablePath=") {
			processPath := strings.TrimPrefix(line, "ExecutablePath=")
			processPath = strings.ToLower(filepath.Clean(processPath))

			if processPath == expectedPath {
				return true
			}
		}
	}

	return false
}

// WaitForTermination polls until the specified process is no longer running
// Returns true if process terminated, false if timeout occurred
func WaitForTermination(processName string, timeout time.Duration) bool {
	start := time.Now()
	for time.Since(start) < timeout {
		cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s", processName), "/NH")
		output, err := cmd.Output()
		if err != nil {
			// If tasklist fails, assume process is not running
			return true
		}

		outputStr := strings.ToLower(string(output))
		if !strings.Contains(outputStr, strings.ToLower(processName)) {
			return true
		}

		time.Sleep(100 * time.Millisecond)
	}
	return false
}
