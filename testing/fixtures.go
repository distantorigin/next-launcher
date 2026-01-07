package testing

import (
	"os"
	"path/filepath"
	"testing"
)

// LoadFixture loads a test fixture file from testdata/
func LoadFixture(t *testing.T, name string) []byte {
	t.Helper()

	// Try current directory first
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err == nil {
		return data
	}

	// Try parent directory (for when running tests from subdirectories)
	path = filepath.Join("..", "testdata", name)
	data, err = os.ReadFile(path)
	if err == nil {
		return data
	}

	t.Fatalf("failed to load fixture %s: %v", name, err)
	return nil
}

// LoadManifestFixture loads a manifest test fixture
func LoadManifestFixture(t *testing.T, name string) []byte {
	t.Helper()
	return LoadFixture(t, filepath.Join("manifests", name))
}

// LoadGitHubFixture loads a GitHub API response fixture
func LoadGitHubFixture(t *testing.T, name string) []byte {
	t.Helper()
	return LoadFixture(t, filepath.Join("github-responses", name))
}

// LoadZipFixture loads a ZIP file test fixture
func LoadZipFixture(t *testing.T, name string) []byte {
	t.Helper()
	return LoadFixture(t, filepath.Join("zips", name))
}

// CreateTestManifest creates a temporary manifest file for testing
func CreateTestManifest(t *testing.T, dir string, content string) string {
	t.Helper()
	manifestPath := filepath.Join(dir, ".manifest")
	WriteFile(t, manifestPath, content)
	return manifestPath
}
