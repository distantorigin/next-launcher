package version

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Version represents the application version
type Version struct {
	Major  int    `json:"major"`
	Minor  int    `json:"minor"`
	Patch  int    `json:"patch"`
	Commit string `json:"commit,omitempty"`
	Date   string `json:"date,omitempty"`
}

// String returns the version in semantic format
func (v Version) String() string {
	ver := fmt.Sprintf("%d.%d.%02d", v.Major, v.Minor, v.Patch)
	if v.Commit != "" {
		ver += "+" + v.Commit
	}
	return ver
}

// ParseTag extracts version components from a git tag (e.g., "v1.2.3")
func ParseTag(tag string) (major, minor, patch int, err error) {
	tagVersion := strings.TrimPrefix(tag, "v")
	parts := strings.Split(tagVersion, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid tag format: %s (expected vX.Y.Z)", tag)
	}

	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid major version in tag %s: %w", tag, err)
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid minor version in tag %s: %w", tag, err)
	}
	patch, err = strconv.Atoi(parts[2])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid patch version in tag %s: %w", tag, err)
	}

	return major, minor, patch, nil
}

// LoadLocal reads version information from a local version.json file
func LoadLocal(baseDir, versionFile string) (*Version, error) {
	path := filepath.Join(baseDir, versionFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read local version: %w", err)
	}

	var v Version
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("failed to parse local version: %w", err)
	}

	return &v, nil
}

// Save writes version information to a version.json file
func Save(baseDir, versionFile string, v *Version) error {
	path := filepath.Join(baseDir, versionFile)
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal version: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write version file: %w", err)
	}

	return nil
}
