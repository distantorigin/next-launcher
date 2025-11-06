package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Release represents a GitHub release
type Release struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	ZipURL  string `json:"zipball_url"`
}

// Ref represents a GitHub reference
type Ref struct {
	Ref    string    `json:"ref"`
	NodeID string    `json:"node_id"`
	URL    string    `json:"url"`
	Object RefObject `json:"object"`
}

// RefObject represents the object field in a GitHub reference
type RefObject struct {
	SHA  string `json:"sha"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

// Tree represents a GitHub tree object
type Tree struct {
	SHA  string     `json:"sha"`
	URL  string     `json:"url"`
	Tree []TreeItem `json:"tree"`
}

// TreeItem represents an item in a GitHub tree
type TreeItem struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int    `json:"size,omitempty"`
	URL  string `json:"url"`
}

// Commit represents a GitHub commit
type Commit struct {
	SHA    string      `json:"sha"`
	Commit CommitInner `json:"commit"`
}

// CommitInner represents the commit details
type CommitInner struct {
	Author    CommitAuthor `json:"author"`
	Committer CommitAuthor `json:"committer"`
	Message   string       `json:"message"`
}

// CommitAuthor represents commit author information
type CommitAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Date  string `json:"date"`
}

// Comparison represents a comparison between two commits
type Comparison struct {
	AheadBy  int      `json:"ahead_by"`
	BehindBy int      `json:"behind_by"`
	Status   string   `json:"status"`
	Commits  []Commit `json:"commits"`
}

// Branch represents a GitHub branch
type Branch struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
		URL string `json:"url"`
	} `json:"commit"`
	Protected bool `json:"protected"`
}

// Client handles GitHub API requests
type Client struct {
	owner      string
	repo       string
	httpClient *http.Client
}

// NewClient creates a new GitHub API client
func NewClient(owner, repo string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}
	return &Client{
		owner:      owner,
		repo:       repo,
		httpClient: httpClient,
	}
}

// SetHTTPClient sets the HTTP client (useful for testing)
func (c *Client) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

// GetHTTPClient returns the HTTP client (useful for testing)
func (c *Client) GetHTTPClient() *http.Client {
	return c.httpClient
}

// GetLatestCommit fetches the latest commit for a given ref
func (c *Client) GetLatestCommit(ref string) (*Commit, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s", c.owner, c.repo, ref)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch commit: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch commit: HTTP %d", resp.StatusCode)
	}

	var commit Commit
	if err := json.NewDecoder(resp.Body).Decode(&commit); err != nil {
		return nil, fmt.Errorf("failed to parse commit: %w", err)
	}

	return &commit, nil
}

// CompareCommits compares two commits and returns the comparison
func (c *Client) CompareCommits(base, head string) (*Comparison, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/compare/%s...%s", c.owner, c.repo, base, head)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to compare commits: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to compare commits: HTTP %d", resp.StatusCode)
	}

	var comparison Comparison
	if err := json.NewDecoder(resp.Body).Decode(&comparison); err != nil {
		return nil, fmt.Errorf("failed to parse comparison: %w", err)
	}

	return &comparison, nil
}

// GetLastCommitDate fetches the last commit date for a given ref
func (c *Client) GetLastCommitDate(ref string) (string, error) {
	commit, err := c.GetLatestCommit(ref)
	if err != nil {
		return "", err
	}
	return commit.Commit.Author.Date, nil
}

// GetLatestTag fetches the latest tag from the repository
func (c *Client) GetLatestTag() (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/refs/tags", c.owner, c.repo)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch tags: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch tags: HTTP %d", resp.StatusCode)
	}

	var refs []Ref
	if err := json.NewDecoder(resp.Body).Decode(&refs); err != nil {
		return "", fmt.Errorf("failed to parse tags: %w", err)
	}

	if len(refs) == 0 {
		return "", fmt.Errorf("no tags found in repository")
	}

	// Get the last tag (most recent)
	lastRef := refs[len(refs)-1]
	// Extract tag name from ref (refs/tags/v1.0.0 -> v1.0.0)
	tagName := lastRef.Ref
	if idx := strings.LastIndex(tagName, "/"); idx >= 0 {
		tagName = tagName[idx+1:]
	}

	return tagName, nil
}

// GetTree fetches the tree object for a given ref
func (c *Client) GetTree(ref string) (*Tree, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", c.owner, c.repo, ref)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tree: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch tree: HTTP %d", resp.StatusCode)
	}

	var tree Tree
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, fmt.Errorf("failed to parse tree: %w", err)
	}

	return &tree, nil
}

// GetRawURL returns the raw URL for a file at a given tag
func (c *Client) GetRawURL(tag string, path string) string {
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", c.owner, c.repo, tag, path)
}

// GetBranches fetches all branches from the repository
func (c *Client) GetBranches() ([]Branch, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/branches?per_page=100", c.owner, c.repo)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch branches: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch branches: HTTP %d", resp.StatusCode)
	}

	var branches []Branch
	if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
		return nil, fmt.Errorf("failed to parse branches: %w", err)
	}

	return branches, nil
}

// FormatCommitAsCliffNote formats a commit message as a cliff note
func FormatCommitAsCliffNote(commit Commit) string {
	message := commit.Commit.Message
	firstLine := strings.Split(message, "\n")[0]

	// Skip merge commits
	if strings.HasPrefix(strings.ToLower(firstLine), "merge ") {
		return ""
	}

	// Try to extract semantic commit type
	var commitType string
	var commitMessage string

	if idx := strings.Index(firstLine, ":"); idx > 0 && idx < 20 {
		commitType = strings.TrimSpace(firstLine[:idx])
		commitMessage = strings.TrimSpace(firstLine[idx+1:])
	} else {
		commitMessage = firstLine
	}

	// Format output
	shortSHA := commit.SHA[:7]
	if commitType != "" {
		return fmt.Sprintf("- [%s] %s (%s)", commitType, commitMessage, shortSHA)
	}
	return fmt.Sprintf("- %s (%s)", commitMessage, shortSHA)
}
