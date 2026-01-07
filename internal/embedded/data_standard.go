//go:build !embedded

package embedded

// Stub implementations for normal builds without embedded data.
// These functions return empty/false values indicating no embedded data.

func hasData() bool {
	return false
}

func getVersion() string {
	return ""
}

func getZipData() []byte {
	return nil
}
