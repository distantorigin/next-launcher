package channel

import (
	"os"
	"path/filepath"
	"strings"
)

const ChannelFile = ".update-channel"

// Save writes the channel to the channel file in the specified directory
func Save(baseDir, channel string) error {
	channelPath := filepath.Join(baseDir, ChannelFile)
	return os.WriteFile(channelPath, []byte(channel), 0644)
}

// Load reads the channel from the channel file in the specified directory
func Load(baseDir string) (string, error) {
	channelPath := filepath.Join(baseDir, ChannelFile)
	data, err := os.ReadFile(channelPath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// IsBuiltIn returns true if the channel is a built-in channel (stable or dev)
func IsBuiltIn(channel string) bool {
	return channel == "stable" || channel == "dev"
}
