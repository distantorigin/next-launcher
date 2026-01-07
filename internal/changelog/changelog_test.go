package changelog

import (
	"strings"
	"testing"

	"github.com/distantorigin/next-launcher/internal/github"
	"github.com/distantorigin/next-launcher/internal/manifest"
)

func TestFormatCommitAsCliffNote(t *testing.T) {
	tests := []struct {
		name     string
		commit   github.Commit
		expected string
	}{
		{
			name: "basic commit",
			commit: github.Commit{
				SHA: "abc1234567890",
				Commit: github.CommitInner{
					Message: "Fix bug in parser",
				},
			},
			expected: "* Fix bug in parser (Commit abc1234)",
		},
		{
			name: "lowercase first letter gets capitalized",
			commit: github.Commit{
				SHA: "def5678901234",
				Commit: github.CommitInner{
					Message: "add new feature",
				},
			},
			expected: "* Add new feature (Commit def5678)",
		},
		{
			name: "multiline commit uses first line only",
			commit: github.Commit{
				SHA: "ghi9012345678",
				Commit: github.CommitInner{
					Message: "Update docs\n\nThis is a longer description\nthat spans multiple lines.",
				},
			},
			expected: "* Update docs (Commit ghi9012)",
		},
		{
			name: "merge commit returns empty",
			commit: github.Commit{
				SHA: "jkl3456789012",
				Commit: github.CommitInner{
					Message: "Merge branch 'feature' into main",
				},
			},
			expected: "",
		},
		{
			name: "merge commit case insensitive",
			commit: github.Commit{
				SHA: "mno6789012345",
				Commit: github.CommitInner{
					Message: "MERGE pull request #123",
				},
			},
			expected: "",
		},
		{
			name: "short SHA unchanged",
			commit: github.Commit{
				SHA: "abc",
				Commit: github.CommitInner{
					Message: "Short sha",
				},
			},
			expected: "* Short sha (Commit abc)",
		},
		{
			name: "whitespace trimmed",
			commit: github.Commit{
				SHA: "pqr0123456789",
				Commit: github.CommitInner{
					Message: "  Trim whitespace  \n",
				},
			},
			expected: "* Trim whitespace (Commit pqr0123)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatCommitAsCliffNote(tt.commit)
			if got != tt.expected {
				t.Errorf("FormatCommitAsCliffNote() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestGenerateCliffNotes(t *testing.T) {
	t.Run("empty commits returns empty string", func(t *testing.T) {
		got := GenerateCliffNotes(nil)
		if got != "" {
			t.Errorf("GenerateCliffNotes(nil) = %q, want empty string", got)
		}

		got = GenerateCliffNotes([]github.Commit{})
		if got != "" {
			t.Errorf("GenerateCliffNotes([]) = %q, want empty string", got)
		}
	})

	t.Run("single commit", func(t *testing.T) {
		commits := []github.Commit{
			{
				SHA:    "abc1234567890",
				Commit: github.CommitInner{Message: "Add feature"},
			},
		}
		got := GenerateCliffNotes(commits)
		if !strings.Contains(got, "Changes in this update:") {
			t.Error("GenerateCliffNotes() missing header")
		}
		if !strings.Contains(got, "* Add feature (Commit abc1234)") {
			t.Error("GenerateCliffNotes() missing commit note")
		}
	})

	t.Run("multiple commits with merge filtered", func(t *testing.T) {
		commits := []github.Commit{
			{
				SHA:    "abc1234567890",
				Commit: github.CommitInner{Message: "Add feature"},
			},
			{
				SHA:    "def5678901234",
				Commit: github.CommitInner{Message: "Merge branch 'main'"},
			},
			{
				SHA:    "ghi9012345678",
				Commit: github.CommitInner{Message: "Fix bug"},
			},
		}
		got := GenerateCliffNotes(commits)
		if !strings.Contains(got, "Add feature") {
			t.Error("GenerateCliffNotes() missing first commit")
		}
		if strings.Contains(got, "Merge branch") {
			t.Error("GenerateCliffNotes() should filter merge commits")
		}
		if !strings.Contains(got, "Fix bug") {
			t.Error("GenerateCliffNotes() missing last commit")
		}
	})
}

func TestBuild(t *testing.T) {
	t.Run("basic changelog with updates and deletes", func(t *testing.T) {
		updates := []manifest.FileInfo{
			{Name: "file1.txt"},
			{Name: "file2.txt"},
		}
		deletedFiles := []string{"old.txt"}

		cfg := BuildConfig{
			Channel: "dev",
		}

		got := Build(updates, deletedFiles, cfg)

		// Check header
		if !strings.Contains(got, "Miriani-Next Update Changelog") {
			t.Error("Build() missing header")
		}
		if !strings.Contains(got, "Channel: dev") {
			t.Error("Build() missing channel")
		}
		if !strings.Contains(got, "Total changes: 3 files (2 updated, 1 deleted)") {
			t.Error("Build() missing or incorrect total changes")
		}

		// Check file lists
		if !strings.Contains(got, "+ file1.txt") {
			t.Error("Build() missing updated file1")
		}
		if !strings.Contains(got, "+ file2.txt") {
			t.Error("Build() missing updated file2")
		}
		if !strings.Contains(got, "- old.txt") {
			t.Error("Build() missing deleted file")
		}
	})

	t.Run("changelog with cliff notes", func(t *testing.T) {
		updates := []manifest.FileInfo{
			{Name: "file1.txt"},
		}

		commits := []github.Commit{
			{
				SHA:    "abc1234567890",
				Commit: github.CommitInner{Message: "Important fix"},
			},
		}

		cfg := BuildConfig{
			Channel: "dev",
			GetCommitsSinceUpdate: func() ([]github.Commit, error) {
				return commits, nil
			},
		}

		got := Build(updates, nil, cfg)

		if !strings.Contains(got, "Changes in this update:") {
			t.Error("Build() missing cliff notes header")
		}
		if !strings.Contains(got, "Important fix") {
			t.Error("Build() missing cliff note content")
		}
	})

	t.Run("empty updates and deletes", func(t *testing.T) {
		cfg := BuildConfig{
			Channel: "stable",
		}

		got := Build(nil, nil, cfg)

		if !strings.Contains(got, "Total changes: 0 files (0 updated, 0 deleted)") {
			t.Error("Build() incorrect count for empty changes")
		}
	})
}
