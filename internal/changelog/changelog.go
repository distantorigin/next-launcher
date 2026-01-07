package changelog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/distantorigin/next-launcher/internal/github"
	"github.com/distantorigin/next-launcher/internal/manifest"
)

// FormatCommitAsCliffNote formats a commit message as a cliff note
func FormatCommitAsCliffNote(commit github.Commit) string {
	message := commit.Commit.Message
	lines := strings.Split(message, "\n")
	firstLine := strings.TrimSpace(lines[0])

	// Skip merge commits
	if strings.HasPrefix(strings.ToLower(firstLine), "merge ") {
		return ""
	}

	// Truncate SHA to 7 characters
	shortSHA := commit.SHA
	if len(shortSHA) > 7 {
		shortSHA = shortSHA[:7]
	}

	// Capitalize first letter
	if len(firstLine) > 0 {
		firstRune := []rune(firstLine)
		firstRune[0] = []rune(strings.ToUpper(string(firstRune[0])))[0]
		firstLine = string(firstRune)
	}

	return fmt.Sprintf("* %s (Commit %s)", firstLine, shortSHA)
}

// GenerateCliffNotes generates cliff notes from a list of commits
func GenerateCliffNotes(commits []github.Commit) string {
	if len(commits) == 0 {
		return ""
	}

	var notes strings.Builder
	notes.WriteString("\nChanges in this update:\n\n")

	for _, commit := range commits {
		note := FormatCommitAsCliffNote(commit)
		if note != "" {
			notes.WriteString(note + "\n")
		}
	}

	return notes.String()
}

// BuildConfig holds configuration for building a changelog
type BuildConfig struct {
	Channel               string
	GetCommitsSinceUpdate func() ([]github.Commit, error)
}

// Build creates a formatted changelog string
func Build(updates []manifest.FileInfo, deletedFiles []string, cfg BuildConfig) string {
	var changelog strings.Builder
	totalChanges := len(updates) + len(deletedFiles)

	changelog.WriteString("Miriani-Next Update Changelog\n\n")
	changelog.WriteString(fmt.Sprintf("Channel: %s\n", cfg.Channel))
	changelog.WriteString(fmt.Sprintf("Update completed: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	changelog.WriteString(fmt.Sprintf("Total changes: %d files (%d updated, %d deleted)\n", totalChanges, len(updates), len(deletedFiles)))

	// Add cliff notes for dev/experimental or changelog.txt for stable
	if cfg.Channel == "stable" {
		changelogPath := filepath.Join("docs", "changelog.txt")
		if content, err := os.ReadFile(changelogPath); err == nil {
			changelog.WriteString("\n")
			changelog.WriteString(strings.Repeat("=", 60))
			changelog.WriteString("\n")
			changelog.WriteString("RELEASE NOTES\n")
			changelog.WriteString(strings.Repeat("=", 60))
			changelog.WriteString("\n\n")
			changelog.WriteString(string(content))
			changelog.WriteString("\n")
			changelog.WriteString(strings.Repeat("=", 60))
			changelog.WriteString("\n\n")
		}
	} else if cfg.GetCommitsSinceUpdate != nil {
		if commits, err := cfg.GetCommitsSinceUpdate(); err == nil && len(commits) > 0 {
			cliffNotes := GenerateCliffNotes(commits)
			if cliffNotes != "" {
				changelog.WriteString("\n")
				changelog.WriteString(cliffNotes)
				changelog.WriteString("\n")
			}
		}
	}

	// Add file list
	changelog.WriteString("\n")
	changelog.WriteString(strings.Repeat("-", 60))
	changelog.WriteString("\nDetailed file changes:\n")
	changelog.WriteString(strings.Repeat("-", 60))
	changelog.WriteString("\n\n")

	if len(updates) > 0 {
		changelog.WriteString(fmt.Sprintf("Updated/Added (%d files):\n", len(updates)))
		for _, update := range updates {
			changelog.WriteString(fmt.Sprintf("  + %s\n", update.Name))
		}
		changelog.WriteString("\n")
	}

	if len(deletedFiles) > 0 {
		changelog.WriteString(fmt.Sprintf("Deleted (%d files):\n", len(deletedFiles)))
		for _, deleted := range deletedFiles {
			changelog.WriteString(fmt.Sprintf("  - %s\n", deleted))
		}
		changelog.WriteString("\n")
	}

	return changelog.String()
}
