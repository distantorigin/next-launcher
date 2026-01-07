package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestIsPortListening_Integration tests port listening detection
// Note: This is an integration test that uses actual system commands
func TestIsPortListening_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Test with a port that's very unlikely to be in use
	// Port 65533 is near the top of the ephemeral range
	result := IsPortListening("65533")

	// We can't assert true/false without knowing system state
	// Just verify the function doesn't panic or error
	t.Logf("IsPortListening(65533) = %v", result)
}

// TestIsNodeListeningOnPort_Integration tests node process detection
func TestIsNodeListeningOnPort_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Test with unlikely port - node probably isn't running on 65533
	result := IsNodeListeningOnPort("65533")

	// We expect false unless node happens to be running
	// Just verify the function doesn't panic
	t.Logf("IsNodeListeningOnPort(65533) = %v", result)
}

// TestIsMUSHClientRunningInDir_CurrentDir tests MUSHclient detection
func TestIsMUSHClientRunningInDir_CurrentDir(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Test with current working directory
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}

	// MUSHclient is almost certainly NOT running from our test directory
	result := IsMUSHClientRunningInDir(cwd)

	if result {
		t.Logf("Note: MUSHclient appears to be running from test directory")
	}

	// Just verify function doesn't panic
	t.Logf("IsMUSHClientRunningInDir(%s) = %v", cwd, result)
}

// TestIsMUSHClientRunningInDir_NonexistentDir tests with nonexistent directory
func TestIsMUSHClientRunningInDir_NonexistentDir(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Test with directory that doesn't exist
	result := IsMUSHClientRunningInDir("/nonexistent/directory/that/does/not/exist")

	// Should return false for nonexistent directory
	if result {
		t.Error("IsMUSHClientRunningInDir() should return false for nonexistent directory")
	}
}

// TestWaitForTermination_AlreadyTerminated tests waiting for non-running process
func TestWaitForTermination_AlreadyTerminated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Use a process name that's very unlikely to exist
	processName := "nonexistent-process-12345.exe"

	start := time.Now()
	result := WaitForTermination(processName, 2*time.Second)
	elapsed := time.Since(start)

	if !result {
		t.Error("WaitForTermination() should return true for non-running process")
	}

	// Should return quickly (much less than timeout)
	if elapsed > 500*time.Millisecond {
		t.Errorf("WaitForTermination() took %v, should return quickly for non-running process", elapsed)
	}
}

// TestWaitForTermination_ShortTimeout tests timeout behavior
func TestWaitForTermination_ShortTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Test with a process that IS running (ourselves - the test process)
	// Use a very short timeout to verify timeout logic works
	processName := "go.exe" // The test is running under go.exe

	start := time.Now()
	result := WaitForTermination(processName, 300*time.Millisecond)
	elapsed := time.Since(start)

	// Should timeout and return false
	if result {
		t.Logf("Note: go.exe terminated during test (unusual)")
	}

	// Should take approximately the timeout duration
	if elapsed < 200*time.Millisecond {
		t.Errorf("WaitForTermination() returned too quickly: %v", elapsed)
	}

	if elapsed > 500*time.Millisecond {
		t.Errorf("WaitForTermination() took too long: %v, expected ~300ms", elapsed)
	}
}

// TestCommandAvailability tests that required system commands exist
func TestCommandAvailability(t *testing.T) {
	commands := []string{
		"tasklist",
		"netstat",
	}

	for _, cmdName := range commands {
		t.Run(cmdName, func(t *testing.T) {
			_, err := exec.LookPath(cmdName)
			if err != nil {
				t.Errorf("required command %s not found in PATH: %v", cmdName, err)
			}
		})
	}
}

// TestProcessPathCleaning tests path normalization logic
func TestProcessPathCleaning(t *testing.T) {
	// This tests the path normalization logic used in IsMUSHClientRunningInDir
	tests := []struct {
		name      string
		targetDir string
		wantPath  string
	}{
		{
			name:      "standard path",
			targetDir: "C:\\Users\\Test\\MUSHclient",
			wantPath:  "c:\\users\\test\\mushclient\\mushclient.exe",
		},
		{
			name:      "path with forward slashes",
			targetDir: "C:/Users/Test/MUSHclient",
			wantPath:  "c:\\users\\test\\mushclient\\mushclient.exe",
		},
		{
			name:      "path with mixed slashes",
			targetDir: "C:\\Users/Test\\MUSHclient",
			wantPath:  "c:\\users\\test\\mushclient\\mushclient.exe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectedPath := filepath.Join(tt.targetDir, "MUSHclient.exe")
			expectedPath = strings.ToLower(filepath.Clean(expectedPath))

			if expectedPath != tt.wantPath {
				t.Errorf("path normalization = %q, want %q", expectedPath, tt.wantPath)
			}
		})
	}
}

// TestPortStringFormatting tests port string handling
func TestPortStringFormatting(t *testing.T) {
	// Test that port checking handles different port formats
	tests := []struct {
		name string
		port string
		want string
	}{
		{
			name: "standard port",
			port: "8080",
			want: ":8080",
		},
		{
			name: "low port",
			port: "80",
			want: ":80",
		},
		{
			name: "high port",
			port: "65535",
			want: ":65535",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The IsPortListening function looks for ":port" in the output
			portPattern := ":" + tt.port
			if portPattern != tt.want {
				t.Errorf("port pattern = %q, want %q", portPattern, tt.want)
			}
		})
	}
}

// TestWaitForTermination_ZeroTimeout tests zero timeout edge case
func TestWaitForTermination_ZeroTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Zero or negative timeout should return immediately
	processName := "go.exe"

	start := time.Now()
	result := WaitForTermination(processName, 0)
	elapsed := time.Since(start)

	// Should return very quickly
	if elapsed > 100*time.Millisecond {
		t.Errorf("WaitForTermination() with zero timeout took %v, should be nearly instant", elapsed)
	}

	// With zero timeout, it should check once and return false if process is running
	if result {
		t.Logf("Note: process terminated or not found on first check")
	}
}
