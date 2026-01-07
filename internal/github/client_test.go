package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestGetLatestCommit_Success tests successful commit retrieval
func TestGetLatestCommit_Success(t *testing.T) {
	expectedCommit := Commit{
		SHA: "abc123def456",
		Commit: CommitInner{
			Author: CommitAuthor{
				Name:  "Test Author",
				Email: "test@example.com",
				Date:  time.Now().Format(time.RFC3339),
			},
			Message: "feat: add new feature",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the correct path for GitHub API
		if !strings.Contains(r.URL.Path, "/repos/") || !strings.Contains(r.URL.Path, "/commits/") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedCommit)
	}))
	defer server.Close()

	// Create client with custom HTTP client that uses our test server
	client := NewClient("owner", "repo", &http.Client{})
	// Note: We can't easily test this without modifying the URLs since they're hardcoded to api.github.com
	// For now, skip this test - it requires refactoring to inject base URL
	t.Skip("requires base URL injection - see internal/github/client.go")

	commit, err := client.GetLatestCommit("main")
	if err != nil {
		t.Fatalf("GetLatestCommit() error = %v", err)
	}

	if commit.SHA != expectedCommit.SHA {
		t.Errorf("GetLatestCommit() SHA = %s, want %s", commit.SHA, expectedCommit.SHA)
	}

	if commit.Commit.Message != expectedCommit.Commit.Message {
		t.Errorf("GetLatestCommit() Message = %s, want %s", commit.Commit.Message, expectedCommit.Commit.Message)
	}
}

// TestFormatCommitAsCliffNote tests commit message formatting
func TestFormatCommitAsCliffNote(t *testing.T) {
	tests := []struct {
		name       string
		commit     Commit
		wantPrefix string
		wantEmpty  bool
	}{
		{
			name: "feature commit with semantic prefix",
			commit: Commit{
				SHA: "abc123def456789",
				Commit: CommitInner{
					Message: "feat: add new feature",
				},
			},
			wantPrefix: "- [feat]",
		},
		{
			name: "fix commit with semantic prefix",
			commit: Commit{
				SHA: "abc123def456789",
				Commit: CommitInner{
					Message: "fix: resolve bug",
				},
			},
			wantPrefix: "- [fix]",
		},
		{
			name: "no semantic prefix",
			commit: Commit{
				SHA: "abc123def456789",
				Commit: CommitInner{
					Message: "update documentation",
				},
			},
			wantPrefix: "- update documentation",
		},
		{
			name: "merge commit should be skipped",
			commit: Commit{
				SHA: "abc123def456789",
				Commit: CommitInner{
					Message: "Merge pull request #123",
				},
			},
			wantEmpty: true,
		},
		{
			name: "merge commit lowercase",
			commit: Commit{
				SHA: "abc123def456789",
				Commit: CommitInner{
					Message: "merge branch 'main' into feature",
				},
			},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatCommitAsCliffNote(tt.commit)

			if tt.wantEmpty {
				if got != "" {
					t.Errorf("FormatCommitAsCliffNote() = %q, want empty string (merge should be skipped)", got)
				}
				return
			}

			if got == "" {
				t.Errorf("FormatCommitAsCliffNote() returned empty, want prefix %q", tt.wantPrefix)
				return
			}

			// Check if result contains expected prefix
			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("FormatCommitAsCliffNote() = %q, want to start with %q", got, tt.wantPrefix)
			}

			// Check that short SHA is included
			shortSHA := tt.commit.SHA[:7]
			if !strings.Contains(got, shortSHA) {
				t.Errorf("FormatCommitAsCliffNote() = %q, want to contain short SHA %q", got, shortSHA)
			}
		})
	}
}

// TestNewClient tests client creation
func TestNewClient(t *testing.T) {
	t.Run("with custom http client", func(t *testing.T) {
		customClient := &http.Client{Timeout: 10 * time.Second}
		client := NewClient("owner", "repo", customClient)

		if client == nil {
			t.Fatal("NewClient() returned nil")
		}

		if client.GetHTTPClient() != customClient {
			t.Error("NewClient() didn't use provided HTTP client")
		}
	})

	t.Run("with nil http client", func(t *testing.T) {
		client := NewClient("owner", "repo", nil)

		if client == nil {
			t.Fatal("NewClient() returned nil")
		}

		if client.GetHTTPClient() == nil {
			t.Error("NewClient() should create default HTTP client when nil is provided")
		}

		// Check default timeout
		if client.GetHTTPClient().Timeout != 30*time.Second {
			t.Errorf("NewClient() default timeout = %v, want 30s", client.GetHTTPClient().Timeout)
		}
	})
}

// TestSetHTTPClient tests setting the HTTP client
func TestSetHTTPClient(t *testing.T) {
	client := NewClient("owner", "repo", nil)

	newHTTPClient := &http.Client{Timeout: 5 * time.Second}
	client.SetHTTPClient(newHTTPClient)

	if client.GetHTTPClient() != newHTTPClient {
		t.Error("SetHTTPClient() didn't update the HTTP client")
	}
}

// TestGetRawURL tests raw URL construction
func TestGetRawURL(t *testing.T) {
	client := NewClient("myowner", "myrepo", nil)

	tests := []struct {
		name string
		tag  string
		path string
		want string
	}{
		{
			name: "simple file",
			tag:  "v1.0.0",
			path: "README.md",
			want: "https://raw.githubusercontent.com/myowner/myrepo/v1.0.0/README.md",
		},
		{
			name: "nested file",
			tag:  "v2.0.0",
			path: "src/main.go",
			want: "https://raw.githubusercontent.com/myowner/myrepo/v2.0.0/src/main.go",
		},
		{
			name: "branch ref",
			tag:  "main",
			path: "docs/guide.md",
			want: "https://raw.githubusercontent.com/myowner/myrepo/main/docs/guide.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.GetRawURL(tt.tag, tt.path)
			if got != tt.want {
				t.Errorf("GetRawURL(%q, %q) = %q, want %q", tt.tag, tt.path, got, tt.want)
			}
		})
	}
}
