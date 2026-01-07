package channel

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSaveAndLoad tests saving and loading channel configuration
func TestSaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()

	tests := []string{"stable", "dev", "feature/test-branch", "custom"}

	for _, channel := range tests {
		t.Run(channel, func(t *testing.T) {
			err := Save(tempDir, channel)
			if err != nil {
				t.Fatalf("Save() error = %v", err)
			}

			loaded, err := Load(tempDir)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if loaded != channel {
				t.Errorf("Load() = %q, want %q", loaded, channel)
			}
		})
	}
}

// TestLoad_TrimsWhitespace tests that whitespace is trimmed from loaded channel
func TestLoad_TrimsWhitespace(t *testing.T) {
	tempDir := t.TempDir()
	channelFile := filepath.Join(tempDir, ChannelFile)

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "leading space",
			content: "  stable",
			want:    "stable",
		},
		{
			name:    "trailing space",
			content: "stable  ",
			want:    "stable",
		},
		{
			name:    "leading and trailing space",
			content: "  dev  ",
			want:    "dev",
		},
		{
			name:    "newline at end",
			content: "stable\n",
			want:    "stable",
		},
		{
			name:    "tabs and spaces",
			content: "\t stable \t\n",
			want:    "stable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write with whitespace
			err := os.WriteFile(channelFile, []byte(tt.content), 0644)
			if err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			loaded, err := Load(tempDir)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if loaded != tt.want {
				t.Errorf("Load() = %q, want %q (whitespace not trimmed)", loaded, tt.want)
			}
		})
	}
}

// TestLoad_FileNotFound tests error handling when file doesn't exist
func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/.update-channel")

	if err == nil {
		t.Error("Load() expected error for nonexistent file, got nil")
	}
}

// TestIsBuiltIn tests built-in channel detection
func TestIsBuiltIn(t *testing.T) {
	tests := []struct {
		channel string
		want    bool
	}{
		{"stable", true},
		{"dev", true},
		{"main", false},
		{"feature/test", false},
		{"custom-branch", false},
		{"", false},
		{"STABLE", false}, // Case sensitive
		{"DEV", false},    // Case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			got := IsBuiltIn(tt.channel)
			if got != tt.want {
				t.Errorf("IsBuiltIn(%q) = %v, want %v", tt.channel, got, tt.want)
			}
		})
	}
}

// TestSave_CreatesFile tests that Save creates the channel file
func TestSave_CreatesFile(t *testing.T) {
	tempDir := t.TempDir()

	err := Save(tempDir, "stable")
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file was created
	channelFile := filepath.Join(tempDir, ChannelFile)
	if _, err := os.Stat(channelFile); os.IsNotExist(err) {
		t.Error("Save() did not create channel file")
	}

	// Verify content
	loaded, err := Load(tempDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded != "stable" {
		t.Errorf("Load() = %q, want %q", loaded, "stable")
	}
}

// TestSave_EmptyChannel tests saving empty channel name
func TestSave_EmptyChannel(t *testing.T) {
	tempDir := t.TempDir()

	err := Save(tempDir, "")
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(tempDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded != "" {
		t.Errorf("Load() = %q, want empty string", loaded)
	}
}
