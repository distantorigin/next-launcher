package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha1"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/effects"
	"github.com/gopxl/beep/speaker"
	"github.com/gopxl/beep/wav"
)

//go:embed sounds/error.wav
var errorSound []byte

//go:embed sounds/downloading.wav
var downloadingSound []byte

//go:embed sounds/installing.wav
var installingSound []byte

//go:embed sounds/success.wav
var successSound []byte

//go:embed sounds/start.wav
var startSound []byte

//go:embed sounds/proxiani.wav
var proxianiSound []byte

//go:embed sounds/up_to_date.wav
var upToDateSound []byte

//go:embed sounds/select.wav
var selectSound []byte

var (
	_ embed.FS // Ensure embed package is recognized by compiler

	speakerInitialized bool
	speakerMutex       sync.Mutex
	backgroundVolume   *effects.Volume
	backgroundMutex    sync.Mutex
)

// Set up Windows API calls to attach or create a console window
var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	user32             = syscall.NewLazyDLL("user32.dll")
	attachConsole      = kernel32.NewProc("AttachConsole")
	allocConsole       = kernel32.NewProc("AllocConsole")
	freeConsole        = kernel32.NewProc("FreeConsole")
	getStdHandle       = kernel32.NewProc("GetStdHandle")
	showWindowProc     = user32.NewProc("ShowWindow")
	SetFocusProc       = user32.NewProc("SetFocus")
	hasConsoleAttached bool
)

const (
	ATTACH_PARENT_PROCESS = ^uint32(0) // -1 as uint32
	STD_INPUT_HANDLE      = ^uint32(0) - 10 + 1
	STD_OUTPUT_HANDLE     = ^uint32(0) - 11 + 1
	STD_ERROR_HANDLE      = ^uint32(0) - 12 + 1
	SW_MAXIMIZE           = 3
)

func ensureSpeakerInitialized(format beep.Format) {
	speakerMutex.Lock()
	defer speakerMutex.Unlock()

	if !speakerInitialized {
		if verboseFlag {
			log.Println("Setting up audio...")
		}
		speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
		speakerInitialized = true
	}
}

func decodeSoundData(soundData []byte) (beep.StreamSeekCloser, beep.Format, error) {
	if len(soundData) == 0 {
		if verboseFlag {
			log.Println("Couldn't play sound (no data)")
		}
		return nil, beep.Format{}, fmt.Errorf("no sound data")
	}

	streamer, format, err := wav.Decode(bytes.NewReader(soundData))
	if err != nil {
		if verboseFlag {
			log.Println("Sound file couldn't be decoded:", err)
		}
		return nil, beep.Format{}, err
	}

	return streamer, format, nil
}

// initConsole tries to show a console window for output. If we're running from
// a command line, it attaches to the parent console. Otherwise, it creates a new one.
func initConsole() bool {
	// Check if we already have a console (normal for non-GUI builds)
	stdOutputHandle, _, _ := getStdHandle.Call(uintptr(STD_OUTPUT_HANDLE))
	if stdOutputHandle != 0 && stdOutputHandle != uintptr(syscall.InvalidHandle) {
		// Already good, we're in a console
		return true
	}

	// Try to attach to parent console (we're running in a terminal)
	attachSuccess, _, _ := attachConsole.Call(uintptr(ATTACH_PARENT_PROCESS))

	wasAllocated := false
	if attachSuccess == 0 {
		// No parent console - we were double-clicked or launched without one
		// Create a new console window for output
		allocSuccess, _, _ := allocConsole.Call()
		if allocSuccess == 0 {
			// Failed to create console, continue without one
			return false
		}
		wasAllocated = true
	}

	// Now grab the handles to stdout, stderr, and stdin
	stdOutputHandle, _, _ = getStdHandle.Call(uintptr(STD_OUTPUT_HANDLE))
	stdErrorHandle, _, _ := getStdHandle.Call(uintptr(STD_ERROR_HANDLE))
	stdInputHandle, _, _ := getStdHandle.Call(uintptr(STD_INPUT_HANDLE))

	if stdOutputHandle != 0 && stdOutputHandle != uintptr(syscall.InvalidHandle) {
		os.Stdout = os.NewFile(stdOutputHandle, "/dev/stdout")
	}
	if stdErrorHandle != 0 && stdErrorHandle != uintptr(syscall.InvalidHandle) {
		os.Stderr = os.NewFile(stdErrorHandle, "/dev/stderr")
	}
	if stdInputHandle != 0 && stdInputHandle != uintptr(syscall.InvalidHandle) {
		os.Stdin = os.NewFile(stdInputHandle, "/dev/stdin")
	}

	// If we created a new console, maximize it and bring it to focus
	if wasAllocated {
		consoleWindowHandle := getConsoleWindow()
		if consoleWindowHandle != 0 {
			showWindowProc.Call(consoleWindowHandle, SW_MAXIMIZE)
			showWindowProc.Call(consoleWindowHandle, 1)
			SetFocusProc.Call(consoleWindowHandle)
		}
	}

	return true
}

func playSound(soundData []byte) {
	if quietFlag {
		return
	}

	streamer, format, err := decodeSoundData(soundData)
	if err != nil {
		return
	}
	defer streamer.Close()

	ensureSpeakerInitialized(format)

	done := make(chan bool)
	speaker.Play(beep.Seq(streamer, beep.Callback(func() {
		done <- true
	})))

	if verboseFlag {
		log.Println("Playing sound...")
	}
	<-done
	if verboseFlag {
		log.Println("Sound finished")
	}
}

func stopAllSounds() {
	if !speakerInitialized {
		return
	}
	speaker.Clear()
}

func playSoundAsync(soundData []byte, volumeDB float64) {
	playSoundAsyncLoop(soundData, volumeDB, false)
}

func playSoundAsyncLoop(soundData []byte, volumeDB float64, loop bool) {
	if quietFlag {
		return
	}

	streamer, format, err := decodeSoundData(soundData)
	if err != nil {
		return
	}

	ensureSpeakerInitialized(format)

	// Loop the sound if requested (for background music)
	var finalStreamer beep.Streamer = streamer
	if loop {
		finalStreamer = beep.Loop(-1, streamer)
	}

	// Wrap the audio with volume control so we can adjust it later if needed
	backgroundMutex.Lock()
	backgroundVolume = &effects.Volume{
		Streamer: finalStreamer,
		Base:     2,
		Volume:   volumeDB,
		Silent:   false,
	}
	backgroundMutex.Unlock()

	// Start playing in background without blocking
	speaker.Play(beep.Seq(backgroundVolume, beep.Callback(func() {
		streamer.Close()
		backgroundMutex.Lock()
		backgroundVolume = nil
		backgroundMutex.Unlock()
	})))

	if verboseFlag {
		if loop {
			log.Println("Started looping background sound...")
		} else {
			log.Println("Started background sound...")
		}
	}
}

func playSoundWithDucking(soundData []byte, foregroundVolumeDB float64) {
	if quietFlag {
		return
	}

	streamer, format, err := decodeSoundData(soundData)
	if err != nil {
		return
	}
	defer streamer.Close()

	ensureSpeakerInitialized(format)

	// Lower the background sound so the foreground sound is more audible
	backgroundMutex.Lock()
	originalVolume := 0.0
	if backgroundVolume != nil {
		originalVolume = backgroundVolume.Volume
		// Gradually reduce background volume over 300ms
		go func() {
			steps := 10
			for i := 0; i < steps; i++ {
				backgroundMutex.Lock()
				if backgroundVolume != nil {
					backgroundVolume.Volume = originalVolume - (5.0 * float64(i) / float64(steps))
				}
				backgroundMutex.Unlock()
				time.Sleep(30 * time.Millisecond)
			}
		}()
	}
	backgroundMutex.Unlock()

	// Wrap foreground sound with volume control
	foregroundVolume := &effects.Volume{
		Streamer: streamer,
		Base:     2,
		Volume:   foregroundVolumeDB,
		Silent:   false,
	}

	done := make(chan bool)
	speaker.Play(beep.Seq(foregroundVolume, beep.Callback(func() {
		done <- true
	})))

	<-done

	// Fade background back up over 500ms
	backgroundMutex.Lock()
	if backgroundVolume != nil {
		go func() {
			steps := 15
			for i := 0; i < steps; i++ {
				backgroundMutex.Lock()
				if backgroundVolume != nil {
					currentVol := originalVolume - 5.0 + (5.0 * float64(i) / float64(steps))
					backgroundVolume.Volume = currentVol
				}
				backgroundMutex.Unlock()
				time.Sleep(33 * time.Millisecond)
			}
			// Ensure we end up exactly at original volume
			backgroundMutex.Lock()
			if backgroundVolume != nil {
				backgroundVolume.Volume = originalVolume
			}
			backgroundMutex.Unlock()
		}()
	}
	backgroundMutex.Unlock()

	if verboseFlag {
		log.Println("Foreground sound finished, fading background back up")
	}
}

const (
	updaterVersion = "1.2.05"
	githubOwner    = "distantorigin"
	githubRepo     = "miriani-next"
	manifestFile   = ".manifest"
	versionFile    = "version.json"
	excludesFile   = ".updater-excludes"
	channelFile    = ".update-channel"
	zipThreshold   = 30
	fileWorkers    = 6
	title = "Miriani"

	// Default Toastush miriani.mcl SHA1 hash (unmodified version)
	defaultToastushMCLHash = "57b5a6a2ace40a151fe3f1e1eddd029189ff9097"
)

var (
	// baseURL is dynamically constructed based on channel
	baseURL string
	// httpClient with connection pooling and timeouts
	httpClient *http.Client
)

var (
	quietFlag               bool
	verboseFlag             bool
	versionFlag             bool
	channelFlag             string
	generateManifest        bool
	nonInteractive          bool
	switchChannel           string
	switchChannelSubcommand bool
	channelExplicitlySet    bool
	allowRestartFlag        bool
	subcommand              string // Current subcommand being executed
)

// ErrUserCancelled is returned when the user cancels an operation
var ErrUserCancelled = fmt.Errorf("operation cancelled by user")

type FileInfo struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
	URL  string `json:"url"`
}

type Version struct {
	Major  int    `json:"major"`
	Minor  int    `json:"minor"`
	Patch  int    `json:"patch"`
	Commit string `json:"commit,omitempty"`
	Date   string `json:"date,omitempty"`
}

type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	ZipURL  string `json:"zipball_url"`
}

type GitHubRef struct {
	Ref    string         `json:"ref"`
	NodeID string         `json:"node_id"`
	URL    string         `json:"url"`
	Object GitHubRefObject `json:"object"`
}

type GitHubRefObject struct {
	SHA  string `json:"sha"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

type GitHubTree struct {
	SHA  string          `json:"sha"`
	URL  string          `json:"url"`
	Tree []GitHubTreeItem `json:"tree"`
}

type GitHubTreeItem struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int    `json:"size,omitempty"`
	URL  string `json:"url"`
}

type GitHubCommit struct {
	SHA    string            `json:"sha"`
	Commit GitHubCommitInner `json:"commit"`
}

type GitHubCommitInner struct {
	Author    GitHubCommitAuthor `json:"author"`
	Committer GitHubCommitAuthor `json:"committer"`
	Message   string             `json:"message"`
}

type GitHubCommitAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Date  string `json:"date"`
}

type GitHubComparison struct {
	AheadBy  int              `json:"ahead_by"`
	BehindBy int              `json:"behind_by"`
	Status   string           `json:"status"`
	Commits  []GitHubCommit   `json:"commits"`
}

// Returns the version in semantic format as a string
func (v Version) String() string {
	ver := fmt.Sprintf("%d.%d.%02d", v.Major, v.Minor, v.Patch)
	if v.Commit != "" {
		ver += fmt.Sprintf("+%s", v.Commit[:7])
	}
	return ver
}

func getLatestCommit(ref string) (*GitHubCommit, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s", githubOwner, githubRepo, ref)
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch commit: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch commit: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read commit data: %w", err)
	}

	var commit GitHubCommit
	if err := json.Unmarshal(data, &commit); err != nil {
		return nil, fmt.Errorf("failed to parse commit data: %w", err)
	}

	return &commit, nil
}

// compareCommits tells us how many commits apart two branches are (who's ahead, who's behind)
func compareCommits(base, head string) (*GitHubComparison, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/compare/%s...%s",
		githubOwner, githubRepo, base, head)
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to compare commits: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to compare commits: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read comparison data: %w", err)
	}

	var comparison GitHubComparison
	if err := json.Unmarshal(data, &comparison); err != nil {
		return nil, fmt.Errorf("failed to parse comparison data: %w", err)
	}

	return &comparison, nil
}

// getLastCommitDate fetches the last commit date for a branch or tag
func getLastCommitDate(ref string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s",
		githubOwner, githubRepo, ref)
	resp, err := httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to get commit info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get commit info: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read commit data: %w", err)
	}

	var commit GitHubCommit
	if err := json.Unmarshal(data, &commit); err != nil {
		return "", fmt.Errorf("failed to parse commit data: %w", err)
	}

	// Parse and format the date
	t, err := time.Parse(time.RFC3339, commit.Commit.Author.Date)
	if err != nil {
		return commit.Commit.Author.Date, nil // Return raw if parsing fails
	}

	return t.Format("Jan 2, 2006"), nil
}

// validateChannelSwitch validates switching from one channel to another
// Returns an error if the switch would be a downgrade, or nil if safe
func validateChannelSwitch(fromChannel, toChannel string) error {
	if fromChannel == "" || fromChannel == toChannel {
		return nil // No switch
	}

	// Switching from dev/experimental to stable
	if toChannel == "stable" && (fromChannel == "dev" || (fromChannel != "stable" && fromChannel != "dev")) {
		if verboseFlag || nonInteractive {
			fmt.Println("Checking if stable is ahead of your current version...")
		}

		latestTag, err := getLatestTag()
		if err != nil {
			return fmt.Errorf("failed to get latest stable tag: %w", err)
		}

		compareBranch := "main"
		if fromChannel != "dev" {
			compareBranch = fromChannel
		}

		comparison, err := compareCommits(compareBranch, latestTag)
		if err != nil {
			return fmt.Errorf("failed to compare commits: %w", err)
		}

		if comparison.BehindBy > 0 {
			fmt.Printf("\nCannot switch to stable - it is older than your current version.\n")
			fmt.Printf("Stable (%s) is %d commits behind %s.\n", latestTag, comparison.BehindBy, fromChannel)
			fmt.Println("\nThis would downgrade your installation, which could cause issues.")
			fmt.Println("\nPlease wait for the next stable release before switching.")
			playSoundAsync(errorSound, 0.0)
			return fmt.Errorf("stable is behind %s, refusing downgrade", fromChannel)
		}

		if comparison.AheadBy > 0 {
			if !quietFlag {
				fmt.Printf("Stable (%s) is %d commits ahead of %s. Safe to switch.\n", latestTag, comparison.AheadBy, fromChannel)
			}
		} else {
			if !quietFlag {
				fmt.Printf("Stable (%s) is at the same commit as %s. Safe to switch.\n", latestTag, fromChannel)
			}
		}
		return nil
	}

	// Switching from stable to dev
	if toChannel == "dev" && fromChannel == "stable" {
		if !quietFlag {
			fmt.Println("Checking if dev is ahead of your current stable version...")
		}

		latestTag, err := getLatestTag()
		if err != nil {
			if !quietFlag {
				fmt.Println("Warning: couldn't check stable version for comparison")
			}
			return nil
		}

		comparison, err := compareCommits("main", latestTag)
		if err != nil {
			if !quietFlag {
				fmt.Println("Warning: couldn't compare dev to stable")
			}
			return nil
		}

		// BehindBy = how many commits the tag is behind main = dev is AHEAD
		// AheadBy = how many commits the tag is ahead of main = dev is BEHIND
		if comparison.AheadBy > 0 {
			// Dev is behind stable!
			fmt.Printf("\nWARNING: Dev (main) is %d commits BEHIND stable (%s).\n", comparison.AheadBy, latestTag)
			fmt.Println("Switching to dev would be a DOWNGRADE.")
			if !confirmAction("Switch to older dev version anyway?") {
				return fmt.Errorf("user cancelled downgrade to dev")
			}
		} else if !quietFlag && comparison.BehindBy > 0 {
			fmt.Printf("Dev is ahead of stable (%s) by %d commits. Safe to switch.\n", latestTag, comparison.BehindBy)
		}
		return nil
	}

	// Switching from experimental to dev/stable?
	if fromChannel != "stable" && fromChannel != "dev" && (toChannel == "dev" || toChannel == "stable") {
		if !quietFlag {
			fmt.Printf("Checking if %s is ahead of your current %s branch...\n", toChannel, fromChannel)
		}

		var targetRef string
		if toChannel == "stable" {
			tag, err := getLatestTag()
			if err != nil {
				return fmt.Errorf("failed to get latest stable tag: %w", err)
			}
			targetRef = tag
		} else {
			targetRef = "main"
		}

		comparison, err := compareCommits(targetRef, fromChannel)
		if err != nil {
			// Non-fatal, just warn
			if !quietFlag {
				fmt.Printf("Warning: couldn't compare %s to %s\n", toChannel, fromChannel)
			}
			return nil
		}

		if comparison.BehindBy > 0 {
			// Target is behind experimental!
			fmt.Printf("\nWARNING: %s is %d commits BEHIND %s.\n", toChannel, comparison.BehindBy, fromChannel)
			fmt.Println("Switching would be a DOWNGRADE.")
			
			if !confirmAction(fmt.Sprintf("Switch to older %s version anyway?", toChannel)) {
				return fmt.Errorf("user cancelled downgrade to %s", toChannel)
			}
		} else if !quietFlag && comparison.AheadBy > 0 {
			fmt.Printf("%s is %d commits ahead of %s. Safe to switch.\n", toChannel, comparison.AheadBy, fromChannel)
		}
		return nil
	}

	return nil
}

func getCommitsSinceLastUpdate() ([]GitHubCommit, error) {
	localVer, err := getLocalVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get local version: %w", err)
	}

	latestVer, err := getLatestVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get latest version: %w", err)
	}

	// Only works for dev/experimental branches with commit info
	if latestVer.Commit == "" {
		return nil, nil
	}

	// If local version has no commit (e.g., just switched from stable), use the branch head
	var baseRef string
	if localVer.Commit == "" {
		// Use the branch name to get all recent commits
		baseRef = channelFlag
		if baseRef == "dev" {
			baseRef = "main"
		}
		// Get last 10 commits from the branch
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits?sha=%s&per_page=10",
			githubOwner, githubRepo, baseRef)
		resp, err := httpClient.Get(url)
		if err != nil {
			return nil, fmt.Errorf("failed to get recent commits: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to get recent commits: HTTP %d", resp.StatusCode)
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read commits: %w", err)
		}

		var commits []GitHubCommit
		if err := json.Unmarshal(data, &commits); err != nil {
			return nil, fmt.Errorf("failed to parse commits: %w", err)
		}

		return commits, nil
	}

	// Compare commits
	comparison, err := compareCommits(localVer.Commit, latestVer.Commit)
	if err != nil {
		return nil, err
	}

	return comparison.Commits, nil
}

// formatCommitAsCliffNote formats a commit message as a cliff note
// Extracts the first line and removes common prefixes/patterns
func formatCommitAsCliffNote(commit GitHubCommit) string {
	// Get first line of commit message
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

	// Capitalize first letter if not already
	if len(firstLine) > 0 {
		firstRune := []rune(firstLine)
		firstRune[0] = []rune(strings.ToUpper(string(firstRune[0])))[0]
		firstLine = string(firstRune)
	}

	return fmt.Sprintf("* %s (Commit %s)", firstLine, shortSHA)
}

func generateCliffNotes(commits []GitHubCommit) string {
	if len(commits) == 0 {
		return ""
	}

	var notes strings.Builder
	notes.WriteString("\nChanges in this update:\n\n")

	for _, commit := range commits {
		note := formatCommitAsCliffNote(commit)
		if note != "" {
			notes.WriteString(note + "\n")
		}
	}

	return notes.String()
}

func getLatestTag() (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/refs/tags", githubOwner, githubRepo)
	resp, err := httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch tags: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch tags: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read tags data: %w", err)
	}

	var refs []GitHubRef
	if err := json.Unmarshal(data, &refs); err != nil {
		return "", fmt.Errorf("failed to parse tags data: %w", err)
	}

	if len(refs) == 0 {
		return "", fmt.Errorf("no tags found in repository")
	}

	// Get the last tag (most recent)
	lastRef := refs[len(refs)-1]
	// Extract tag name from ref (refs/tags/v1.0.0 -> v1.0.0)
	tagName := strings.TrimPrefix(lastRef.Ref, "refs/tags/")

	return tagName, nil
}

func getZipURLForChannel() (string, error) {
	if channelFlag == "stable" {
		tag, err := getLatestTag()
		if err != nil {
			return "", fmt.Errorf("failed to get latest tag: %w", err)
		}
		return fmt.Sprintf("%s/archive/refs/tags/%s.zip", baseURL, tag), nil
	} else if channelFlag == "dev" {
		return fmt.Sprintf("%s/archive/refs/heads/main.zip", baseURL), nil
	}
	// For custom branches
	return fmt.Sprintf("%s/archive/refs/heads/%s.zip", baseURL, channelFlag), nil
}

func getGitHubTree(ref string) (*GitHubTree, error) {
	// Get tree recursively
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1",
		githubOwner, githubRepo, ref)

	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tree: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch tree: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read tree data: %w", err)
	}

	var tree GitHubTree
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("failed to parse tree data: %w", err)
	}

	return &tree, nil
}

func getRawURLForTag(tag string, path string) string {
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
		githubOwner, githubRepo, tag, path)
}

func normalizePath(p string) string {
	return strings.ReplaceAll(filepath.Clean(p), string(filepath.Separator), "/")
}

func denormalizePath(p string) string {
	return strings.ReplaceAll(p, "/", string(filepath.Separator))
}

func isUserConfigFile(path string) bool {
	normalizedPath := strings.ToLower(normalizePath(path))

	// User configuration files that should never be overwritten
	userFiles := []string{
		"mushclient_prefs.sqlite",
		"mushclient.ini",
	}

	for _, userFile := range userFiles {
		if normalizedPath == userFile {
			return true
		}
	}

	// .mcl files in worlds directory
	if strings.HasPrefix(normalizedPath, "worlds/") && strings.HasSuffix(normalizedPath, ".mcl") {
		return true
	} else if strings.HasPrefix(normalizedPath, "worlds/plugins/state/") {
		return true
	} else if strings.HasPrefix(normalizedPath, "logs/") {
		return true
		} else if strings.HasPrefix(normalizedPath, "worlds/settings/") {
		return true
	}

	return false
}

func logProgress(format string, args ...interface{}) {
	if !quietFlag {
		fmt.Printf(format+"\n", args...)
	}
}

type UpdateResult struct {
	Result       string   `json:"result"`        // "success" or "failure"
	Message      string   `json:"message,omitempty"` // Error message if failure
	Version      string   `json:"version,omitempty"` // Full version string if success
	FilesAdded   []string `json:"files_added,omitempty"` // Array of added/updated file paths
	FilesDeleted []string `json:"files_deleted,omitempty"` // Array of deleted file paths
	Restarted    bool     `json:"restarted"`     // Whether MUSHclient was restarted
}

func writeUpdateSuccess(updates []FileInfo, deletedFiles []string, wasRestarted bool) error {
	baseDir, err := os.Getwd()
	if err != nil {
		return err
	}

	// Get the current version
	versionStr := "unknown"
	if latestVer, err := getLatestVersion(); err == nil {
		versionStr = latestVer.String()
	}

	// Build lists of added/updated and deleted file paths
	filesAdded := make([]string, 0, len(updates))
	for _, update := range updates {
		filesAdded = append(filesAdded, update.Name)
	}

	result := UpdateResult{
		Result:       "success",
		Version:      versionStr,
		FilesAdded:   filesAdded,
		FilesDeleted: deletedFiles,
		Restarted:    wasRestarted,
	}

	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal update result: %w", err)
	}

	resultPath := filepath.Join(baseDir, ".update-result")
	return os.WriteFile(resultPath, append(jsonData, '\n'), 0644)
}

// On case-insensitive filesystems, returns the actual case of the file
func findActualPath(targetPath string) (string, error) {
	// First try the path as-is
	if _, err := os.Stat(targetPath); err == nil {
		return targetPath, nil
	}

	// If not found and we're on Windows/case-insensitive, try case-insensitive search
	dir := filepath.Dir(targetPath)
	filename := filepath.Base(targetPath)

	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory doesn't exist, return original path for creation
		return targetPath, nil
	}

	// Search for matching filename (case-insensitive on case-insensitive systems)
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), filename) {
			return filepath.Join(dir, entry.Name()), nil
		}
	}

	// Not found, return original path for creation
	return targetPath, nil
}

func setConsoleTitle(title string) error {
	if !hasConsoleAttached {
		return nil
	}
	kernel32, err := syscall.LoadLibrary("kernel32.dll")
	if err != nil {
		return err
	}
	defer syscall.FreeLibrary(kernel32)

	setConsoleTitleProc, err := syscall.GetProcAddress(kernel32, "SetConsoleTitleW")
	if err != nil {
		return err
	}

	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return err
	}

	r1, _, err := syscall.Syscall(setConsoleTitleProc, 1, uintptr(unsafe.Pointer(titlePtr)), 0, 0)
	if r1 == 0 {
		return fmt.Errorf("SetConsoleTitle failed: %v", err)
	}

	return nil
}

// getConsoleWindow gets the window handle for the console so dialogs appear on top of it
func getConsoleWindow() uintptr {
	kernel32, err := syscall.LoadLibrary("kernel32.dll")
	if err != nil {
		return 0
	}
	defer syscall.FreeLibrary(kernel32)

	getConsoleWindowProc, err := syscall.GetProcAddress(kernel32, "GetConsoleWindow")
	if err != nil {
		return 0
	}

	consoleWindowHandle, _, _ := syscall.Syscall(getConsoleWindowProc, 0, 0, 0, 0)
	return consoleWindowHandle
}

func main() {
	// Global panic handler to prevent path leakage in error messages
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\nOops, something broke: %v\n", r)
			fmt.Fprintln(os.Stderr, "Let the developers know what happened.")
			playSound(errorSound)
			os.Exit(1)
		}
	}()

	// Configure log package to not include file paths
	log.SetFlags(0)

	// Check for subcommands before parsing flags
	var subcommandArgs []string
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		subcommand = os.Args[1]
		subcommandArgs = os.Args[2:]
	}

	// Parse flags FIRST so we know if we're in non-interactive mode
	defaultChannel := "stable"
	flag.StringVar(&channelFlag, "channel", defaultChannel, "Update channel: stable or dev")
	flag.BoolVar(&quietFlag, "quiet", false, "Suppress output")
	flag.BoolVar(&verboseFlag, "verbose", false, "Show detailed output including every file")
	flag.BoolVar(&versionFlag, "version", false, "Show updater version and exit")
	flag.BoolVar(&generateManifest, "generate-manifest", false, "Generate manifest file for current directory")
	flag.BoolVar(&nonInteractive, "non-interactive", false, "Non-interactive mode: log to file, no prompts, write .update-success")
	flag.BoolVar(&allowRestartFlag, "allow-restart", false, "Allow restart in non-interactive mode (use with -non-interactive)")
	
	// Only parse flags if not using subcommand syntax
	if subcommand == "" {
		flag.Parse()
	} else {
		// Parse flags from subcommand args
		// Separate flags from positional args since flag.Parse stops at first non-flag
		var flagArgs []string
		var positionalArgs []string
		for _, arg := range subcommandArgs {
			if strings.HasPrefix(arg, "-") {
				flagArgs = append(flagArgs, arg)
			} else {
				positionalArgs = append(positionalArgs, arg)
			}
		}

		// Parse flags first
		flag.CommandLine.Parse(flagArgs)

		// Manually set flag.Args() to the positional args by parsing them again
		// This is a workaround since we can't directly set flag.Args()
		flag.CommandLine.Parse(append(flagArgs, positionalArgs...))
	}

	// Initialize console attachment after parsing flags
	// Always attach to console for output (even in non-interactive mode)
	hasConsoleAttached = initConsole()
	// Clean up console on exit
	defer func() {
		if hasConsoleAttached {
			freeConsole.Call()
		}
	}()

	setConsoleTitle(title)
	// Clean up old updater binary if this is a post-update restart
	if os.Getenv("UPDATER_CLEANUP_OLD") == "1" {
		if exePath, err := os.Executable(); err == nil {
			oldExe := exePath + ".old"
			// Retry removal a few times with delays (Windows might have file locked)
			for i := 0; i < 3; i++ {
				if err := os.Remove(oldExe); err == nil {
					break
				}
				if i < 2 {
					time.Sleep(500 * time.Millisecond)
				}
			}
		}
		// Clear the environment variable so it doesn't persist
		os.Unsetenv("UPDATER_CLEANUP_OLD")
	}

	// Handle subcommands
	switch subcommand {
	case "check":
		// Check subcommand - handled after initialization
	case "switch":
		// Get channel from first remaining arg after flags
		if len(flag.Args()) > 0 {
			switchChannel = flag.Args()[0]
		} else {
			switchChannel = "" // Will prompt interactively
		}
		switchChannelSubcommand = true
	case "":
		// No subcommand, continue normally
	default:
		fmt.Printf("Unknown subcommand: %s\n", subcommand)
		fmt.Println("\nAvailable subcommands:")
		fmt.Println("  check                    Check for updates only")
		fmt.Println("  switch [stable|dev]      Switch update channel (prompts if no channel specified)")
		fmt.Println("\nOr run without subcommand to update")
		os.Exit(1)
	}

	// Check if channel was explicitly set
	channelExplicitlySet = false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "channel" {
			channelExplicitlySet = true
		}
	})

	// Check for environment variable to allow restart
	if os.Getenv("UPDATER_ALLOW_RESTART") == "1" {
		allowRestartFlag = true
	}

	// Initialize HTTP client with connection pooling and timeouts (needed early for self-update)
	httpClient = &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true, // Required for GitHub archive downloads (already compressed)
		},
	}

	// Load channel before check command (so check uses correct channel)
	if !channelExplicitlySet {
		if loadedChannel, err := loadChannel(); err == nil {
			channelFlag = loadedChannel
			if !quietFlag && verboseFlag {
				fmt.Printf("Using saved channel: %s\n", channelFlag)
			}
		}
	}

	// Validate channel BEFORE check command (so invalid channels get fixed)
	if channelFlag != "stable" && channelFlag != "dev" {
		// Check if it's a valid branch
		if !isValidChannel(channelFlag) {
			// Branch doesn't exist, fall back to dev
			oldChannel := channelFlag
			channelFlag = "dev"

			// Save the fallback channel immediately
			if err := saveChannel(channelFlag); err != nil {
				fmt.Printf("Warning: failed to save channel preference: %v\n", err)
			} else {
				// Only print success message if save worked
				if !quietFlag {
					fmt.Printf("\nThe experimental branch '%s' no longer exists!\n", oldChannel)
					fmt.Printf("Automatically switching you to the 'dev' channel.\n")
					fmt.Printf("You'll now receive updates from the main development branch.\n\n")
				}
			}
		} else {
			// It's a custom branch
			if !quietFlag && !verboseFlag {
				fmt.Printf("WARNING: Using experimental branch: %s\n", channelFlag)
			}
		}
	}

	// Handle check subcommand early (after httpClient init and channel load)
	if subcommand == "check" {
		updates, deletedFiles, err := getPendingUpdates()
		if err != nil {
			fatalError("Error checking updates: %v", err)
		}
		printCheckOutput(updates, deletedFiles)
		return
	}

	// If version flag is set, print version and exit
	if versionFlag {
		fmt.Printf("Miriani-Next Updater v%s\n", updaterVersion)
		return
	}

	// Self-update logic (fails silently with short timeout)
	selfUpdate()

	// If generating manifest, do that and exit
	if generateManifest {
		if err := saveManifest(); err != nil {
			fatalError("Failed to generate manifest: %v", err)
		}
		return
	}


	if switchChannelSubcommand {
		var newChannel string

		// If a channel value was provided, validate and use it
		if switchChannel == "stable" || switchChannel == "dev" {
			newChannel = switchChannel
			fmt.Printf("Switching to %s channel...\n", newChannel)
		} else if switchChannel == "" {
			// No value provided
			if nonInteractive {
				// In non-interactive mode, require channel to be specified
				fmt.Println("Error: Channel must be specified in non-interactive mode.")
				fmt.Println("Usage: updater switch <stable|dev>")
				os.Exit(1)
			}
			// Prompt interactively
			newChannel = promptForChannel()
		} else {
			// Invalid value provided
			fmt.Printf("Error: Invalid channel '%s'. Must be 'stable' or 'dev'.\n", switchChannel)
			playSoundAsync(errorSound, 0.0)
			if !nonInteractive {
				waitForUser("\nPress Enter to exit...")
			}
			os.Exit(1)
		}

		// Load current channel and validate the switch
		currentChannel, _ := loadChannel()
		if err := validateChannelSwitch(currentChannel, newChannel); err != nil {
			if !nonInteractive {
				waitForUser("\nPress Enter to exit...")
			}
			os.Exit(1)
		}
		if err := saveChannel(newChannel); err != nil {
			fatalError("Failed to save channel preference: %v", err)
		}
		fmt.Printf("\nUpdate channel changed to: %s\n", newChannel)
		fmt.Println("Run the updater again to update using the new channel.")

		if !nonInteractive {
			waitForUser("\nPress Enter to exit...")
		}
		return
		
	}

	// If no channel flag was explicitly set, try to load saved channel
	var savedChannel string
	if !channelExplicitlySet {
		if loadedChannel, err := loadChannel(); err == nil {
			savedChannel = loadedChannel
			channelFlag = savedChannel
			if !quietFlag && verboseFlag {
				fmt.Printf("Using saved channel: %s\n", channelFlag)
			}
		}
		// If no saved channel and not installed, prompt for channel during installation
		// (handled in handleInstallation)
	} else {
		// Channel was explicitly set, remember what was saved before
		savedChannel, _ = loadChannel()
	}

	// Set baseURL
	baseURL = fmt.Sprintf("https://github.com/%s/%s", githubOwner, githubRepo)

	if  verboseFlag && !quietFlag {
		if channelFlag == "stable" {
			if tag, err := getLatestTag(); err == nil {
				fmt.Printf("Latest available: %s\n", tag)
			}
		} else {

			if commit, err := getLatestCommit("main"); err == nil {
				fmt.Printf("Latest available: %s (commit %s)\n",
					commit.Commit.Committer.Date, commit.SHA[:7])
			}
		}
	}

	if !isInstalled() {
		// Not installed in current directory
		usr, _ := os.UserHomeDir()
		expectedInstallDir := filepath.Join(usr, "Documents", "Miriani-Next")

		// Check if installation exists in expected location
		existingInstallFound := false
		if _, err := os.Stat(filepath.Join(expectedInstallDir, "MUSHclient.exe")); err == nil {
			existingInstallFound = true
		}

		// Check for a Toastush installation
		toastushPath := detectToastushInstallation()


		playSoundAsync(startSound, 0.0)

		var choice string
		if !nonInteractive {
			choice = promptInstallationMenu(existingInstallFound, expectedInstallDir, toastushPath)
		} else {
			// Non-interactive mode: auto-detect behavior
			if toastushPath != "" {
				choice = "3" // Migrate from Toastush
			} else if existingInstallFound {
				choice = "2" // Install updater to existing installation
			} else {
				choice = "1" // Fresh install
			}
		}

		switch choice {
		case "1":
			// Full installation
			installDir, err := handleInstallation()
			if err != nil {
				// Check if user cancelled
				if err == ErrUserCancelled {
					fmt.Println("Exiting in 3 seconds...")
					time.Sleep(3 * time.Second)
					return
				}
				// Other errors are fatal
				fatalError("Installation failed: %v", err)
			}

			// Launch MUSHclient after successful installation

			// Give a moment for background sounds to finish
			time.Sleep(500 * time.Millisecond)

			// Play success sound (blocks until sound finishes)
			playSound(successSound)

			// Change to install directory and launch
			if err := os.Chdir(installDir); err != nil {
				if !quietFlag && verboseFlag {
					fmt.Printf("Warning: couldn't change to install directory: %v\n", err)
				}
			}

			// Try to launch MUSHclient
			if !quietFlag {
				fmt.Println("Attempting to launch MUSHclient...")
			}
			if err := launchMUSHClient(); err != nil {
				fmt.Printf("Failed to launch MUSHclient: %v\n", err)
				fmt.Printf("Working directory: %s\n", installDir)
				waitForUser("\nPress Enter to exit...")
				return
			}
			return

		case "2":
			// Install updater to existing installation
			installDir := expectedInstallDir

			// If we didn't auto-detect an installation, prompt for the directory
			if !existingInstallFound {
				if !nonInteractive {
					fmt.Println("\nLocate your existing Miriani-Next installation")
					selectedDir, err := promptForInstallFolder(expectedInstallDir)
					if err != nil {
						fmt.Printf("Error selecting folder: %v\n", err)
						waitForUser("\nPress Enter to exit...")
						return
					}
					installDir = selectedDir

					// Verify it's a valid installation
					if _, err := os.Stat(filepath.Join(installDir, "MUSHclient.exe")); os.IsNotExist(err) {
						fmt.Printf("\nMUSHclient.exe not found in: %s\n", installDir)
						fmt.Println("This doesn't appear to be a valid Miriani-Next installation.")
						playSound(errorSound)
						waitForUser("\nPress Enter to exit...")
						return
					}
				} else {
					// Non-interactive mode but no installation found
					logProgress("No existing installation found and cannot prompt for location in non-interactive mode")
					return
				}
			} else {
				// Auto-detected installation - confirm with user
				if !nonInteractive {
					fmt.Printf("\nFound existing installation at: %s\n", installDir)
					if !confirmAction("Install updater to this location?") {
						fmt.Println("\nLocate your Miriani-Next installation")
						selectedDir, err := promptForInstallFolder(expectedInstallDir)
						if err != nil {
							fmt.Printf("Error selecting folder: %v\n", err)
							waitForUser("\nPress Enter to exit...")
							return
						}
						installDir = selectedDir

						// Verify it's a valid installation
						if _, err := os.Stat(filepath.Join(installDir, "MUSHclient.exe")); os.IsNotExist(err) {
							fmt.Printf("\nMUSHclient.exe not found in: %s\n", installDir)
							fmt.Println("This doesn't appear to be a valid Miriani-Next installation.")
							playSound(errorSound)
							waitForUser("\nPress Enter to exit...")
							return
						}
					}
				}
			}

			// Check if updater already exists
			updaterInInstallDir := filepath.Join(installDir, "update.exe")
			if _, err := os.Stat(updaterInInstallDir); err == nil {
				fmt.Printf("\nUpdater already exists at: %s\n", installDir)
				fmt.Println("Please run the updater from that directory.")
				playSound(errorSound)
				waitForUser("\nPress Enter to exit...")
				return
			}

			// If no channel was explicitly set and no saved channel, prompt for selection
			if !channelExplicitlySet && !nonInteractive {
				if _, err := loadChannel(); err != nil {
					// No saved channel in existing install, prompt user
					channelFlag = promptForChannel()
				}
			}

			// Copy updater to installation
			if err := copyUpdaterToInstallation(installDir); err != nil {
				fmt.Printf("Error copying updater: %v\n", err)
				playSound(errorSound)
				waitForUser("\nPress Enter to exit...")
				return
			}

			// Check if manifest is missing and generate if needed
			manifestPath := filepath.Join(installDir, manifestFile)
			if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
				if !quietFlag {
					fmt.Println("Generating manifest...")
				}
				// Change to install directory to generate manifest
				if err := os.Chdir(installDir); err == nil {
					if err := saveManifest(); err != nil {
						fmt.Printf("Warning: failed to generate manifest: %v\n", err)
					} else if !quietFlag {
						fmt.Println("Manifest generated successfully!")
					}
				}
			}

			// Change to install directory
			originalDir, _ := os.Getwd()
			if err := os.Chdir(installDir); err != nil {
				fmt.Printf("Warning: failed to change to install directory: %v\n", err)
			}

			// Save channel preference
			if err := saveChannel(channelFlag); err != nil {
				fmt.Printf("Warning: failed to save channel preference: %v\n", err)
			}

			// Create .updater-excludes file to protect user configuration
			if err := createUpdaterExcludes(); err != nil {
				fmt.Printf("Warning: failed to create .updater-excludes: %v\n", err)
			} else if !quietFlag && verboseFlag {
				fmt.Println("Created .updater-excludes file to protect user configuration")
			}

			// Create channel switching batch files
			if err := createChannelSwitchBatchFiles(installDir); err != nil {
				fmt.Printf("Warning: failed to create channel switch batch files: %v\n", err)
			} else if !quietFlag {
				fmt.Println("Created channel switching batch files")
			}

			fmt.Printf("\nUpdater installed successfully to: %s\n", installDir)

			// Run the updater from the new location to get them up to date
			if !nonInteractive {
				// Check if MUSHclient is running
				if isMUSHClientRunning() {
					fmt.Println("\nMUSHclient is currently running.")
					fmt.Println("Please close MUSHclient before running the updater.")
					playSound(errorSound)
					waitForUser("\nPress Enter to exit...")
					return
				}

				fmt.Println("\nRunning updater to check for updates...")
				updaterPath := filepath.Join(installDir, "update.exe")
				cmd := exec.Command(updaterPath)
				cmd.Dir = installDir
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				cmd.Stdin = os.Stdin
				if err := cmd.Run(); err != nil {
					fmt.Printf("Warning: failed to run updater: %v\n", err)
					playSoundAsync(errorSound, 0.0)
					waitForUser("\nPress Enter to exit...")
				}
				return
			}

			// Restore original directory
			if originalDir != "" {
				os.Chdir(originalDir)
			}

			playSound(successSound)
			waitForUser("\nPress Enter to exit...")
			return

		case "3":
			// Migrate from Toastush
			if err := handleToastushMigration(toastushPath); err != nil {
				// Check if user cancelled
				if err == ErrUserCancelled {
					fmt.Println("Exiting in 5 seconds...")
					time.Sleep(5 * time.Second)
					return
				}
				// Other errors are fatal
				fatalError("Migration failed: %v", err)
			}

			// Get the new installation directory (after rename)
			installDir := filepath.Join(usr, "Documents", "Miriani-Next")
			if toastushPath != "" {
				// Use the renamed directory
				installDir = filepath.Join(filepath.Dir(toastushPath), "Miriani-Next")
			}

			// Give a moment for background sounds to finish
			time.Sleep(500 * time.Millisecond)

			// Play success sound
			playSound(successSound)

			// Change to install directory and launch
			if err := os.Chdir(installDir); err != nil {
				if !quietFlag && verboseFlag {
					fmt.Printf("Warning: couldn't change to install directory: %v\n", err)
				}
			}

			// Try to launch MUSHclient
			if !quietFlag {
				fmt.Println("Attempting to launch MUSHclient...")
			}
			if err := launchMUSHClient(); err != nil {
				fmt.Printf("Failed to launch MUSHclient: %v\n", err)
				fmt.Printf("Working directory: %s\n", installDir)
				waitForUser("\nPress Enter to exit...")
				return
			}
			return

		default:
			fmt.Println("Installation cancelled.")
			waitForUser("\nPress Enter to exit...")
			return
		}
	}

	// If we get here, we're installed in the current directory
	if err := cleanOldFolder(); err != nil {
		if !quietFlag && verboseFlag {
			fmt.Printf("Warning: failed to clean .old directory: %v\n", err)
		}
	}

	// Check if we're switching channels and if it would be a downgrade
	if err := validateChannelSwitch(savedChannel, channelFlag); err != nil {
		waitForUser("\nPress Enter to exit...")
		return
	}

	updates, deletedFiles, err := getPendingUpdates()
	if err != nil {
		fatalError("Error checking updates: %v", err)
	}

	if len(updates) == 0 && len(deletedFiles) == 0 {
		fmt.Println("Already up to date!")
		if !quietFlag {
			playSoundAsync(upToDateSound, 0.0)
		}
		return
	}

	if !quietFlag && !nonInteractive {
		totalChanges := len(updates) + len(deletedFiles)
		fmt.Printf("\n%d files will be changed (%d updates, %d deletions).\n", totalChanges, len(updates), len(deletedFiles))
	}

	// Check if MUSHclient is running - only if we need to update MUSHclient.exe itself
	mushWasRunning := false
	restartRequired := needsToUpdateMUSHClientExe(updates)

	// In non-interactive mode, check if restart is required without allow-restart flag
	if nonInteractive && restartRequired && !allowRestartFlag {
		// Check if MUSHclient is running
		if isMUSHClientRunning() {
			fmt.Println("restart required")
			return
		}
	}

	if restartRequired && isMUSHClientRunning() {
		if nonInteractive {
			// In non-interactive mode with allow-restart, kill MUSHclient before updating
			if allowRestartFlag {
				logProgress("MUSHclient is running. Killing MUSHclient to proceed with update...")
				if err := exec.Command("taskkill", "/IM", "MUSHclient.exe", "/F").Run(); err != nil {
					logProgress("Error: failed to kill MUSHclient: %v", err)
					return
				}
				mushWasRunning = true
				logProgress("MUSHclient killed successfully. Proceeding with update...")
				playSoundAsync(successSound, 0.0)
				// Wait a moment for process to fully terminate
				time.Sleep(1 * time.Second)
			} else {
				// This shouldn't happen since we checked above, but handle it anyway
				fmt.Println("restart required")
				return
			}
		} else {
			// In interactive mode, tell user to close it
			fmt.Println("\nMUSHclient is running and needs to be closed to update it.")
			fmt.Println("MUSHclient.exe needs to be updated, but it is currently running.")
			fmt.Println("Please close MUSHclient and run the updater again.")
			playSoundAsync(errorSound, 0.0)
			waitForUser("\nPress Enter to exit...")
			return
		}
	}

	// Ask for confirmation before updating
	if !confirmAction("Do you want to proceed with the update?") {
		fmt.Println("Update cancelled.")
		waitForUser("Press Enter to exit...")
		return
	}

	if err := performUpdates(updates); err != nil {
		fatalError("Error updating: %v", err)
	}

	// Perform deletions for files that are no longer in the manifest
	baseDir, err := os.Getwd()
	if err != nil {
		fatalError("Error getting working directory: %v", err)
	}
	for _, path := range deletedFiles {
		filePath := filepath.Join(baseDir, denormalizePath(path))
		if err := moveToOldFolder(filePath, path); err == nil {
			if !quietFlag && verboseFlag && !nonInteractive {
				fmt.Printf("Removed: %s (moved to .old/)\n", path)
			}
		}
	}

	// Save current version after successful update
	// This updates the local .current_version file to match what we just downloaded
	if latestVer, err := getLatestVersion(); err == nil {
		if versionData, err := json.MarshalIndent(latestVer, "", "  "); err == nil {
			os.WriteFile(versionFile, versionData, 0644)
		}
	}

	// Show changelog
	if (len(updates) > 0 || len(deletedFiles) > 0) && !quietFlag && !nonInteractive {
		showChangelog(updates, deletedFiles)
	}

	// After update, restart MUSHclient if we killed it
	if mushWasRunning {
		logProgress("Restarting MUSHclient...")
		if err := launchMUSHClient(); err != nil {
			logProgress("Warning: failed to restart MUSHclient: %v", err)
			if !quietFlag && !nonInteractive {
				fmt.Printf("Warning: failed to restart MUSHclient: %v\n", err)
			}
		} else {
			logProgress("MUSHclient restarted successfully.")
			if !quietFlag && !nonInteractive {
				fmt.Println("MUSHclient restarted.")
			}
		}
	} else if !mushWasRunning {
		// Launch MUSHclient if it wasn't running before
		if err := launchMUSHClient(); err != nil {
			if !nonInteractive {
				fmt.Printf("Warning: failed to launch MUSHclient: %v\n", err)
			}
		} else if !quietFlag && !nonInteractive {
			fmt.Println("MUSHclient launched.")
		}
	}

	playSound(successSound)
	if !quietFlag && !nonInteractive {
		fmt.Println("\nUpdate complete!")
	}

	// Write .update-result file in non-interactive mode
	if nonInteractive {
		if err := writeUpdateSuccess(updates, deletedFiles, mushWasRunning); err != nil {
			logProgress("Warning: failed to write .update-result: %v", err)
		}
	}
}
	
	// selfUpdate checks for a new version of the updater itself and replaces it if a new version is available.
	// This function fails silently with a short timeout to avoid blocking the main update process.
	func selfUpdate() error {
		const updaterVersionURL = "https://anomalousabode.com/next/updater-version"
	const updaterBinaryURL = "https://anomalousabode.com/next/updater"

		// Get the path of the current executable
		exePath, err := os.Executable()
		if err != nil {
			// Silent failure - not critical
			return nil
		}

		// Create a client with a very short timeout for self-update check (3 seconds)
		quickClient := &http.Client{
			Timeout: 500 * time.Millisecond,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     10 * time.Second,
				DisableCompression:  false,
			},
		}

		// Make a request to the update URL
		resp, err := quickClient.Get(updaterVersionURL)
		if err != nil {
			// Silent failure - network issues, server down, etc.
			return nil
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			// Silent failure - updater not available or other HTTP error
			return nil
		}

		// Read the remote version
		versionData, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil
		}

		remoteVersion := strings.TrimSpace(string(versionData))
		if remoteVersion == "" || remoteVersion == updaterVersion {
			return nil
		}

		// Update available - notify user
		if !quietFlag && !nonInteractive {
			fmt.Printf("\nAn update for the updater is available!\n")
			fmt.Printf("Current: %s. New version: %s\n\n", updaterVersion, remoteVersion)
			fmt.Printf("Downloading and installing update...\n")
			playSoundAsyncLoop(downloadingSound, 0.0, true)
		}

		// Download new binary (use longer timeout)
		downloadClient := &http.Client{Timeout: 120 * time.Second}
		binaryResp, err := downloadClient.Get(updaterBinaryURL)
		if err != nil {
			if !quietFlag && !nonInteractive {
				fmt.Printf("Download failed: %v\n", err)
				playSound(errorSound)
			}
			return nil
		}
		defer binaryResp.Body.Close()

		if binaryResp.StatusCode != http.StatusOK {
			return nil
		}

		data, err := io.ReadAll(binaryResp.Body)
		if err != nil {
			return nil
		}

 		// Rename running exe, write new one, restart
		oldExe := exePath + ".old"
		os.Remove(oldExe)
		if err := os.Rename(exePath, oldExe); err != nil {
			return nil
		}

		if err := os.WriteFile(exePath, data, 0755); err != nil {
			os.Rename(oldExe, exePath)
			return nil
		}

		if !quietFlag && !nonInteractive {
			fmt.Printf("Update installed! Restarting...\n\n")
			playSound(successSound)
			time.Sleep(1 * time.Second)
		}

		// Restart with same arguments
		cmd := exec.Command(exePath, os.Args[1:]...)

		// On Windows, we need to properly detach the child process
		// Inherit stdin/stdout/stderr so the new process shows in the same console
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		// Set environment variable to signal cleanup of .old file
		cmd.Env = append(os.Environ(), "UPDATER_CLEANUP_OLD=1")

		// Start the new process
		if err := cmd.Start(); err != nil {
			if !quietFlag && !nonInteractive {
				fmt.Printf("Failed to restart updater: %v\n", err)
			}
			// Restore old executable if restart failed
			os.Remove(exePath)
			os.Rename(oldExe, exePath)
			return err
		}

		// Give the new process a moment to initialize before we exit
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)

		return nil
	}
	
// ------------------------
// UPDATES
// ------------------------
func getPendingUpdates() ([]FileInfo, []string, error) {
	localManifest, err := loadLocalManifest()
	if err != nil {
		// If manifest is missing or corrupted but we're in an installation directory, auto-generate it from local files
		if hasWorldFilesInCurrentDir() {
			if errors.Is(err, os.ErrNotExist) {
				if !quietFlag {
					fmt.Println("Manifest missing. Generating manifest from local files...")
				}
			} else {
				if !quietFlag {
					fmt.Printf("Manifest corrupted (%v). Regenerating from local files...\n", err)
				}
			}
			if err := saveManifest(); err != nil {
				return nil, nil, fmt.Errorf("failed to generate local manifest: %w", err)
			}
			// Try loading again after generation
			localManifest, err = loadLocalManifest()
			if err != nil {
				return nil, nil, err
			}
		} else {
			return nil, nil, err
		}
	}

	remoteManifest, err := loadRemoteManifest()
	if err != nil {
		return nil, nil, err
	}
	excludes := loadExcludes()

	// Normalize local manifest keys for case-insensitive comparison
	normalizedLocal := make(map[string]FileInfo)
	for path, info := range localManifest {
		normalized := normalizePath(path)
		normalizedLocal[normalized] = info
	}

	var updates []FileInfo
	for path, remote := range remoteManifest {
		normalized := normalizePath(path)
		// Check if file matches any exclusion pattern
		if matchesExclusionPattern(normalized, excludes) {
			if !quietFlag && verboseFlag {
				fmt.Printf("Skipping excluded file: %s\n", normalized)
			}
			continue
		}

		// Check if file is in local manifest
		local, existsLocally := normalizedLocal[normalized]

		// Need update if: file doesn't exist in local manifest, or hash mismatch
		if !existsLocally {
			updates = append(updates, remote)
		} else if local.Hash != remote.Hash {
			updates = append(updates, remote)
		}
	}

	// Clean up files that were in local manifest but removed from remote
	var deletedFiles []string
	if !quietFlag && verboseFlag {
		fmt.Println("Checking for removed files...")
	}
	for path := range normalizedLocal {
		// If file is in local manifest but not in remote manifest, mark it for deletion
		if _, existsInRemote := remoteManifest[path]; !existsInRemote {
			deletedFiles = append(deletedFiles, path)
		}
	}

	return updates, deletedFiles, nil
}

// printCheckOutput shows what updates are available (either human-readable or machine format)
func printCheckOutput(updates []FileInfo, deletedFiles []string) {
	hasUpdates := len(updates) > 0 || len(deletedFiles) > 0
	totalChanges := len(updates) + len(deletedFiles)
	restartRequired := needsToUpdateMUSHClientExe(updates)

	// Get version information
	latestVer, err := getLatestVersion()
	localVer, localErr := getLocalVersion()

	if nonInteractive {
if !isInstalled() {
			fmt.Println("Update available: Unknown")
			fmt.Println("Status: Not installed")
}else if !isValidChannel(channelFlag) {
fmt.Println("Status: channel invalid")
	} else if hasUpdates {
			fmt.Println("Update available: Yes")
			if err == nil {
				fmt.Printf("Version: %s\n", latestVer.String())
			}
			if localErr == nil {
				fmt.Printf("Current version: %s\n", localVer.String())
			}
			if restartRequired {
				fmt.Println("Restart required: Yes")
			} else {
				fmt.Println("Restart required: No")
			}
			fmt.Printf("Changes: %d\n", totalChanges)
			fmt.Printf("Updates: %d\n", len(updates))
			fmt.Printf("Deletions: %d\n", len(deletedFiles))
			playSoundAsync(upToDateSound, 0.0)
		} else {
			// No updates - minimal output: just status and current version
			fmt.Println("Update available: No")
			if localErr == nil {
				fmt.Printf("Version: %s\n", localVer.String())
			}
		}
	} else {
		// Human-readable output for interactive mode
		if hasUpdates {
			fmt.Printf("\nAn update is available with %d total changes.\n", totalChanges)
			if len(updates) > 0 {
				fmt.Printf("  • %d files will be updated\n", len(updates))
			}
			if len(deletedFiles) > 0 {
				fmt.Printf("  • %d files will be deleted\n", len(deletedFiles))
			}
			if localErr == nil && err == nil {
				fmt.Printf("\nCurrent version: %s\n", localVer.String())
				fmt.Printf("New version: %s\n", latestVer.String())
			}
			if restartRequired {
				fmt.Println("\nNote: This update requires MUSHclient to be restarted.")
			}
			fmt.Println("\nRun the updater again without 'check' to install the update.")
		} else {
			if !quietFlag {
				playSoundAsync(upToDateSound, 0.0)
			}
			fmt.Println("\nYou're already up to date!")
			if localErr == nil {
				fmt.Printf("Current version: %s\n", localVer.String())
			}
		}
	}
}

func performUpdates(updates []FileInfo) error {
	// We already checked if MUSHclient was running earlier in main()

	// If it's a fresh install or lots of files changed, download as one big zip file for speed.
	// Otherwise, download files individually in parallel.
	useZip := !isInstalled() || len(updates) > zipThreshold

	if useZip {
		return downloadZipAndExtract(updates)
	}

	// Download files in parallel (up to fileWorkers at a time)
	sem := make(chan struct{}, fileWorkers)
	var wg sync.WaitGroup
	var updateMutex sync.Mutex
	var downloadErrors []error
	var completedCount int
	total := len(updates)

	if nonInteractive {
		fmt.Println("Downloading...")
	} else if !quietFlag {
		fmt.Printf("\nDownloading %d files...\n", total)
	}

	for i, u := range updates {
		wg.Add(1)
		sem <- struct{}{}
		go func(info FileInfo, idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := downloadFile(info); err != nil {
				updateMutex.Lock()
				downloadErrors = append(downloadErrors, err)
				updateMutex.Unlock()
			} else {
				updateMutex.Lock()
				completedCount++
				current := completedCount
				updateMutex.Unlock()

				percentage := (current * 100) / total
				// Update title bar with progress
				setConsoleTitle(fmt.Sprintf("%s - Downloading: %d%%", title, percentage))

				if nonInteractive {
					// In non-interactive mode, only print percentage
					fmt.Printf("%d%%\n", percentage)
				} else if !quietFlag {
					if verboseFlag {
						fmt.Printf("[%d/%d] (%d%%) %s\n", current, total, percentage, info.Name)
					} else {
						// Show progress without individual file names - single line update
						fmt.Printf("\rProgress: %d/%d (%d%%)    ", current, total, percentage)
					}
				}
			}
		}(u, i)
	} 
	wg.Wait()

	if !quietFlag && !verboseFlag && !nonInteractive {
		fmt.Printf("\n") // New line after progress
	}

	if len(downloadErrors) > 0 {
		return fmt.Errorf("failed to update %d files: %v", len(downloadErrors), downloadErrors[0])
	}

	if !quietFlag && !nonInteractive {
		fmt.Println("Saving manifest...")
	}
	// Reset title
	setConsoleTitle(title)
	return saveManifest()
}

func downloadFile(info FileInfo) error {
	// Never overwrite user configuration files
	if isUserConfigFile(info.Name) {
		if verboseFlag {
			log.Printf("Skipping user config file: %s\n", info.Name)
		}
		return nil
	}

	resp, err := httpClient.Get(info.URL)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", info.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %s: HTTP %d", info.Name, resp.StatusCode)
	}

	baseDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Normalize the file path from manifest (forward slashes) to platform format
	relativePath := denormalizePath(info.Name)
	targetPath := filepath.Join(baseDir, relativePath)

	// Ensure target path doesn't escape the base directory
	absTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("failed to resolve path for %s: %w", info.Name, err)
	}
	absBaseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("failed to resolve base directory: %w", err)
	}
	if !strings.HasPrefix(absTargetPath, absBaseDir) {
		return fmt.Errorf("path traversal attempt detected: %s", info.Name)
	}

	// Find actual path in case of case mismatch
	targetPath, err = findActualPath(absTargetPath)
	if err != nil {
		return fmt.Errorf("failed to find path for %s: %w", info.Name, err)
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", info.Name, err)
	}

	out, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", info.Name, err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file %s: %w", info.Name, err)
	}
	return nil
}

func downloadAndExtractZip(zipURL string, targetDir string, isInstall bool, filesToExtract []FileInfo) error {
	if nonInteractive {
		fmt.Println("Downloading...")
	} else if !quietFlag {
		fmt.Printf("Downloading archive...\n")
		fmt.Printf("Downloading: %s", zipURL)
	}
	// Play downloading sound during fresh installation download
	if isInstall {
		playSoundAsyncLoop(downloadingSound, 0.0, true) // Normal volume for downloading sound, looping
	}

	resp, err := httpClient.Get(zipURL)
	if err != nil {
		return fmt.Errorf("failed to download archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download archive: HTTP %d", resp.StatusCode)
	}

	// Get content length for progress
	contentLength := resp.ContentLength

	// Read with progress updates
	var zipData []byte
	if contentLength > 0 {
		zipData = make([]byte, 0, contentLength)
		buf := make([]byte, 32*1024) // 32KB chunks
		var downloaded int64

		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				zipData = append(zipData, buf[:n]...)
				downloaded += int64(n)
				percentage := (downloaded * 100) / contentLength
				setConsoleTitle(fmt.Sprintf("%s - Downloading: %d%%", title, percentage))

				if nonInteractive {
					fmt.Printf("%d%%\n", percentage)
				} else if !quietFlag && !verboseFlag {
					fmt.Printf("\rDownloading: %d%%    ", percentage)
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("failed to read archive data: %w", err)
			}
		}
		if !quietFlag && !verboseFlag && !nonInteractive {
			fmt.Printf("\n")
		}
	} else {
		// No content length - download with byte count progress instead
		buf := make([]byte, 32*1024) // 32KB chunks
		var downloaded int64
		lastReportTime := time.Now()

		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				zipData = append(zipData, buf[:n]...)
				downloaded += int64(n)

				if time.Since(lastReportTime) >= 1500*time.Millisecond {
					currentMB := int(downloaded / (1024 * 1024))
					 if !quietFlag && !verboseFlag {
						fmt.Printf("\r%d MB    ", currentMB)
					}
					lastReportTime = time.Now()
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("failed to read archive data: %w", err)
			}
		}
		if !quietFlag && !verboseFlag && !nonInteractive {
			fmt.Printf("\n")
		}
	}

	if nonInteractive {
		fmt.Println("Extracting...")
	} else if !quietFlag {
		fmt.Printf("Extracting files...\n")
	}

	// Stop any currently playing sounds (like download music) before starting extraction
	stopAllSounds()

	// Play installing sound during extraction (for fresh installs)
	if isInstall {
		playSoundAsyncLoop(installingSound, -1.5, true) // Slightly lower volume for installing sound, looping
	}

	r, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("failed to parse archive: %w", err)
	}

	absTargetDir, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("failed to resolve target directory: %w", err)
	}

	// GitHub ZIP archives include a top-level directory named "repo-branch"
	// We need to strip this prefix when extracting
	var stripPrefix string
	if len(r.File) > 0 {
		// Detect the strip prefix from the first file
		firstPath := r.File[0].Name
		if idx := strings.Index(firstPath, "/"); idx != -1 {
			stripPrefix = firstPath[:idx+1]
		}
	}

	// Build a map of files to extract for quick lookup (if filtering is enabled)
	var extractFilter map[string]bool
	if len(filesToExtract) > 0 {
		extractFilter = make(map[string]bool, len(filesToExtract))
		for _, f := range filesToExtract {
			normalizedPath := normalizePath(f.Name)
			extractFilter[normalizedPath] = true
		}
	}

	totalFiles := len(r.File)
	extractedFiles := 0
	skippedFiles := 0
	lastReportedPercentage := -1

	for _, f := range r.File {
		// Strip the GitHub repo-branch prefix
		relPath := f.Name
		if stripPrefix != "" && strings.HasPrefix(relPath, stripPrefix) {
			relPath = strings.TrimPrefix(relPath, stripPrefix)
		}

		// Skip if nothing left after stripping
		if relPath == "" {
			continue
		}

		// If we have a file filter (for updates), skip files not in the filter
		if extractFilter != nil {
			normalizedRelPath := normalizePath(relPath)
			if !extractFilter[normalizedRelPath] {
				skippedFiles++
				if verboseFlag && !nonInteractive {
					fmt.Printf("Skipping (not needed): %s\n", relPath)
				}
				continue
			}
		}

		// Skip user configuration files during updates (but not during fresh install)
		if !isInstall && isUserConfigFile(relPath) {
			// Check if file already exists - only skip if it exists
			filePath := filepath.Join(absTargetDir, denormalizePath(relPath))
			if _, err := os.Stat(filePath); err == nil {
				if !quietFlag && verboseFlag && !nonInteractive {
					fmt.Printf("Preserving existing user config file: %s\n", relPath)
				}
				continue
			}
			// File doesn't exist, install it even though it's a config file
		}

		// Archive paths use forward slashes, normalize to platform format
		normalizedPath := denormalizePath(relPath)
		fpath := filepath.Join(absTargetDir, normalizedPath)

		// Security: Ensure path doesn't escape base directory
		absFpath, err := filepath.Abs(fpath)
		if err != nil {
			return fmt.Errorf("failed to resolve path for %s: %w", relPath, err)
		}
		if !strings.HasPrefix(absFpath, absTargetDir) {
			return fmt.Errorf("path traversal attempt detected in archive: %s", relPath)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(absFpath, f.Mode()); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", absFpath, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(absFpath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", absFpath, err)
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open file in archive %s: %w", relPath, err)
		}

		out, err := os.OpenFile(absFpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return fmt.Errorf("failed to create file %s: %w", absFpath, err)
		}

		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return fmt.Errorf("failed to write file %s: %w", absFpath, err)
		}

		extractedFiles++
		percentage := (extractedFiles * 100) / totalFiles
		// Update title bar with progress
		setConsoleTitle(fmt.Sprintf("%s - Extracting: %d%%", title, percentage))

		if nonInteractive {
			// Only print at meaningful intervals to avoid spam
			// Scale interval based on number of files: more files = finer granularity
			var interval int
			if totalFiles < 100 {
				interval = 25 // 25%, 50%, 75%, 100%
			} else if totalFiles < 1000 {
				interval = 10 // 10%, 20%, 30%...
			} else {
				interval = 5 // 5%, 10%, 15%...
			}

			if percentage != lastReportedPercentage && (percentage%interval == 0 || percentage == 100) {
				fmt.Printf("%d%%\n", percentage)
				lastReportedPercentage = percentage
			}
		} else if !quietFlag {
			if verboseFlag {
				fmt.Printf("[%d/%d] (%d%%) %s\n", extractedFiles, totalFiles, percentage, relPath)
			} else {
				// Single line progress update
				fmt.Printf("\rProgress: %d/%d (%d%%)    ", extractedFiles, totalFiles, percentage)
			}
		}
	}

	if !quietFlag && !nonInteractive {
		if !verboseFlag {
			fmt.Printf("\n") // New line after progress
		}
		if extractFilter != nil {
			fmt.Printf("Extraction complete! (%d files extracted, %d skipped)\n", extractedFiles, skippedFiles)
		} else {
			fmt.Println("Extraction complete!")
		}
	}

	// Reset title
	setConsoleTitle(title)
	return nil
}

func downloadZipAndExtract(updates []FileInfo) error {
	zipURL, err := getZipURLForChannel()
	if err != nil {
		return err
	}

	baseDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	if err := downloadAndExtractZip(zipURL, baseDir, false, updates); err != nil {
		return err
	}

	if !quietFlag && !nonInteractive {
		fmt.Println("Saving manifest...")
	}
	return saveManifest()
}

// ------------------------
// MANIFEST
// ------------------------
func loadLocalManifest() (map[string]FileInfo, error) {
	baseDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}
	path := filepath.Join(baseDir, manifestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read local manifest: %w", err)
	}

	// Strip comment lines (lines starting with //) before parsing JSON
	lines := strings.Split(string(data), "\n")
	var jsonLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip lines that start with // (comments)
		if !strings.HasPrefix(trimmed, "//") {
			jsonLines = append(jsonLines, line)
		}
	}
	cleanedData := strings.Join(jsonLines, "\n")

	var manifest map[string]FileInfo
	if err := json.Unmarshal([]byte(cleanedData), &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse local manifest: %w", err)
	}
	return manifest, nil
}

func loadRemoteManifest() (map[string]FileInfo, error) {
	var ref string

	if channelFlag == "stable" {
		// For stable, get latest tag
		tag, err := getLatestTag()
		if err != nil {
			return nil, fmt.Errorf("failed to get latest tag: %w", err)
		}
		ref = tag
		if !quietFlag && verboseFlag {
			fmt.Printf("Using stable tag: %s\n", tag)
		}
	} else if channelFlag == "dev" {
		// For dev, use main branch (latest commit)
		ref = "main"
		if !quietFlag && verboseFlag {
			fmt.Printf("Using dev: main branch (latest commit)\n")
		}
	} else {
		// For custom branches, use the branch name directly
		ref = channelFlag
		if !quietFlag && verboseFlag {
			fmt.Printf("Using experimental branch: %s\n", ref)
		}
	}

	// Get tree from GitHub API
	tree, err := getGitHubTree(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to get file tree: %w", err)
	}

	// Convert tree to manifest format
	manifest := make(map[string]FileInfo)
	for _, item := range tree.Tree {
		// Only include files (blobs), not directories (trees)
		if item.Type != "blob" {
			continue
		}

		// Skip excluded files
		if shouldExcludeFromManifest(item.Path) {
			continue
		}

		// Normalize path
		normalizedPath := normalizePath(item.Path)

		// Generate raw URL
		rawURL := getRawURLForTag(ref, item.Path)

		manifest[normalizedPath] = FileInfo{
			Name: normalizedPath,
			Hash: item.SHA, // Git SHA-1 hash from GitHub API
			URL:  rawURL,
		}
	}

	if !quietFlag && verboseFlag {
		fmt.Printf("Found %d files in repository\n", len(manifest))
	}

	return manifest, nil
}

func shouldExcludeFromManifest(path string) bool {
	// Normalize the path for case-insensitive comparison
	normalizedPath := strings.ToLower(normalizePath(path))

	excludeList := []string{
		".git/",
		".github/",
		".gitignore",
		".manifest",
		".updater-excludes",
		"worlds/plugin/state/",
		"update.exe",
		"updater.exe",
		"launcher.exe",
		"version.json",
		"mushclient_prefs.sqlite",
		"mushclient.ini",
	}

	for _, pattern := range excludeList {
		patternNormalized := strings.ToLower(pattern)
		if strings.HasPrefix(normalizedPath, patternNormalized) || normalizedPath == strings.TrimSuffix(patternNormalized, "/") {
			return true
		}
	}

	// Exclude .mcl files in worlds directory (user configuration files)
	if strings.HasPrefix(normalizedPath, "worlds/") && strings.HasSuffix(normalizedPath, ".mcl") {
		return true
	}

	return false
}


func saveManifest() error {
	// Get remote manifest (from GitHub API)
	remoteManifest, err := loadRemoteManifest()
	if err != nil {
		return fmt.Errorf("failed to load remote manifest: %w", err)
	}

	baseDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Only save files to local manifest that exist both in remote AND locally on disk
	// This ensures the local manifest accurately represents what's actually installed
	localManifest := make(map[string]FileInfo)
	for path, info := range remoteManifest {
		filePath := filepath.Join(baseDir, denormalizePath(path))
		if _, err := os.Stat(filePath); err == nil {
			// File exists locally, include it in the local manifest
			localManifest[path] = info
		}
	}

	// Save to local file
	data, err := json.MarshalIndent(localManifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := os.WriteFile(filepath.Join(baseDir, manifestFile), append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("failed to save manifest: %w", err)
	}

	return nil
}

// ------------------------
// INSTALLATION
// ------------------------
func handleInstallation() (string, error) {
	// Determine default installation directory
	usr, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	defaultInstallDir := filepath.Join(usr, "Documents", "Miriani-Next")

	fmt.Println("Welcome to the Miriani-Next installer.\n")

	// If no channel was explicitly set, prompt for selection during fresh install
	if !channelExplicitlySet && !nonInteractive {
		channelFlag = promptForChannel()
	}

	// Determine installation directory
	installDir := defaultInstallDir

	// Ask if user wants to change the default location
	if !nonInteractive {
		fmt.Printf("\nDefault installation location: %s\n", defaultInstallDir)
		if confirmAction("Do you want to change the installation location?") {
			selectedDir, err := promptForInstallFolder(defaultInstallDir)
			if err != nil {
				fmt.Printf("Error selecting folder: %v\n", err)
				fmt.Printf("Using default location: %s\n", defaultInstallDir)
			} else {
				installDir = selectedDir
			}
		}
	}

	fmt.Printf("\nThis will install the %s version to: %s\n", channelFlag, installDir)

	// Check if MUSHclient is running before installation
	if isMUSHClientRunning() {
		if nonInteractive {
			// In non-interactive mode, kill MUSHclient before installing
			logProgress("MUSHclient is running. Killing MUSHclient before installation...")
			if err := exec.Command("taskkill", "/IM", "MUSHclient.exe", "/F").Run(); err != nil {
				logProgress("Error: failed to kill MUSHclient: %v", err)
				return "", fmt.Errorf("failed to kill MUSHclient: %w", err)
			}
			logProgress("MUSHclient killed successfully. Proceeding with installation...")
			// Wait a moment for process to fully terminate
			time.Sleep(1 * time.Second)
		} else {
			// In interactive mode, tell user to close it
			fmt.Println("\nMUSHclient is running and needs to be closed to update it.")
			fmt.Println("Please close MUSHclient before proceeding with installation.")
			playSound(errorSound)
			waitForUser("\nPress Enter to exit...")
			return "", fmt.Errorf("MUSHclient is running")
		}
	}

	if !confirmAction("Do you want to proceed with the installation?") {
		fmt.Println("Installation cancelled.")
		return "", ErrUserCancelled
	}

	// Create installation directory
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create installation directory: %w", err)
	}

	if !quietFlag {
		fmt.Printf("\nInstalling to: %s\n", installDir)
	}

	// Get the appropriate zipball
	zipURL, err := getZipURLForChannel()
	if err != nil {
		return "", err
	}

	if !quietFlag && verboseFlag {
		if channelFlag == "stable" {
			tag, _ := getLatestTag()
			fmt.Printf("Installing from tag: %s\n", tag)
		} else if channelFlag == "dev" {
			fmt.Println("Installing from main branch (latest commit)")
		} else {
			fmt.Printf("Installing from experimental branch: %s\n", channelFlag)
		}
	}

	// Download and extract the archive (isInstall = true, no file filter = extract all)
	if err := downloadAndExtractZip(zipURL, installDir, true, nil); err != nil {
		return "", fmt.Errorf("failed to download installation: %w", err)
	}

	// Change to installation directory for manifest save
	originalDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	if err := os.Chdir(installDir); err != nil {
		return "", fmt.Errorf("failed to change to installation directory: %w", err)
	}

	// Save a local manifest for future updates
	if !quietFlag {
		fmt.Println("Saving manifest...")
	}
	if err := saveManifest(); err != nil {
		// Non-fatal - just warn
		fmt.Printf("Warning: failed to save manifest: %v\n", err)
	}

	// Save channel preference
	if err := saveChannel(channelFlag); err != nil {
		// Non-fatal - just warn
		fmt.Printf("Warning: failed to save channel preference: %v\n", err)
	} else if !quietFlag && verboseFlag {
		fmt.Printf("Saved channel preference: %s\n", channelFlag)
	}

	// Save version.json with the installed version
	if latestVer, err := getLatestVersion(); err == nil {
		if versionData, err := json.MarshalIndent(latestVer, "", "  "); err == nil {
			if err := os.WriteFile(versionFile, versionData, 0644); err != nil {
				fmt.Printf("Warning: failed to save version file: %v\n", err)
			} else if !quietFlag && verboseFlag {
				fmt.Printf("Saved version: %s\n", latestVer.String())
			}
		}
	}

	// Create .updater-excludes file to protect user configuration
	if err := createUpdaterExcludes(); err != nil {
		// Non-fatal - just warn
		fmt.Printf("Warning: failed to create .updater-excludes: %v\n", err)
	} else if !quietFlag && verboseFlag {
		fmt.Println("Created .updater-excludes file")
	}

	// Create channel switching batch files
	if err := createChannelSwitchBatchFiles(installDir); err != nil {
		// Non-fatal - just warn
		fmt.Printf("Warning: failed to create channel switch batch files: %v\n", err)
	} else if !quietFlag && verboseFlag {
		fmt.Println("Created channel switching batch files (switch-to-stable.bat, switch-to-dev.bat)")
	}

	if !quietFlag {
		fmt.Println("\nInstallation complete!")
		fmt.Println("Location:", installDir)
	}

	// Check for MUDMixer or Proxiani and offer to configure world file
	// Prioritize MUDMixer if both are running
	proxianiDetected := isProxianiRunning()
	mudmixerDetected := isMUDMixerRunning()

	if (proxianiDetected || mudmixerDetected) && !nonInteractive {
		if mudmixerDetected {
			// Play sound first, then wait before showing messages
			go playSoundWithDucking(proxianiSound, 0.3)
			time.Sleep(300 * time.Millisecond)

			fmt.Println("\nMUDMixer detected!")
			fmt.Println("MUDMixer is a local proxy server that can provide additional features.")
			fmt.Println("Would you like to configure Miriani-Next to connect through MUDMixer?")
			fmt.Println("(This changes the connection from toastsoft.net to localhost:7788)")

			if confirmAction("Configure Miriani to use MUDMixer?") {
				worldFilePath := filepath.Join(installDir, "worlds", "miriani.mcl")
				if err := updateWorldFileForMUDMixer(worldFilePath); err != nil {
					fmt.Printf("Warning: failed to update world file for MUDMixer: %v\n", err)
				} else {
					fmt.Println("World file updated successfully!")
					fmt.Println("Miriani-Next will now connect through MUDMixer (localhost:7788)")
				}
			} else {
				fmt.Println("Skipping MUDMixer configuration. You can manually change this later.")
			}
		} else if proxianiDetected {
			// Play sound first, then wait before showing messages
			go playSoundWithDucking(proxianiSound, 0.3)
			time.Sleep(300 * time.Millisecond)

			fmt.Println("\nProxiani detected!")
			fmt.Println("Proxiani is a local proxy server that can provide additional features.")
			fmt.Println("Would you like to configure Miriani-Next to connect through Proxiani?")
			fmt.Println("(This changes the connection from toastsoft.net to localhost:1234)")

			if confirmAction("Configure Miriani to use Proxiani?") {
				worldFilePath := filepath.Join(installDir, "worlds", "miriani.mcl")
				if err := updateWorldFileForProxiani(worldFilePath); err != nil {
					fmt.Printf("Warning: failed to update world file for Proxiani: %v\n", err)
				} else {
					fmt.Println("World file updated successfully!")
					fmt.Println("Miriani-Next will now connect through Proxiani (localhost:1234)")
				}
			} else {
				fmt.Println("Skipping Proxiani configuration. You can manually change this later.")
			}
		}
	} else if (proxianiDetected || mudmixerDetected) && nonInteractive {
		// In non-interactive mode, auto-configure (prioritize MUDMixer)
		if mudmixerDetected {
			logProgress("MUDMixer detected! Auto-configuring world file...")
			worldFilePath := filepath.Join(installDir, "worlds", "miriani.mcl")
			if err := updateWorldFileForMUDMixer(worldFilePath); err != nil {
				logProgress("Warning: failed to update world file for MUDMixer: %v", err)
			} else {
				logProgress("World file updated successfully for MUDMixer")
			}
		} else if proxianiDetected {
			logProgress("Proxiani detected! Auto-configuring world file...")
			worldFilePath := filepath.Join(installDir, "worlds", "miriani.mcl")
			if err := updateWorldFileForProxiani(worldFilePath); err != nil {
				logProgress("Warning: failed to update world file for Proxiani: %v", err)
			} else {
				logProgress("World file updated successfully for Proxiani")
			}
		}
	}

	// Create desktop icon (wrapped in panic recovery to prevent COM crashes)
	func() {
		defer func() {
			if r := recover(); r != nil {
				if !quietFlag {
					fmt.Printf("Warning: failed to create desktop icon: %v\n", r)
				}
			}
		}()
		if err := createDesktopIcon(installDir); err != nil {
			if !quietFlag {
				fmt.Printf("Warning: failed to create desktop icon: %v\n", err)
			}
		} else if !quietFlag {
			fmt.Println("Desktop shortcut created!")
		}
	}()

	// Move updater to installation directory (AFTER everything is done)
	exePath, err := os.Executable()
	if err == nil {
		// Always name the destination file "update.exe"
		destPath := filepath.Join(installDir, "update.exe")
		// Only move if not already in install dir with correct name
		absExePath, _ := filepath.Abs(exePath)
		absDestPath, _ := filepath.Abs(destPath)

		if absExePath != absDestPath {
			// Go back to original directory before moving
			os.Chdir(originalDir)

			// Copy first, then remove original only if copy succeeded
			data, err := os.ReadFile(exePath)
			if err == nil {
				if err := os.WriteFile(destPath, data, 0755); err == nil {
					// Successfully copied, now safe to remove original
					os.Remove(exePath)
				}
			}
		}
	}

	return installDir, nil
}

// ------------------------
// UPDATER MANAGEMENT
// ------------------------
func copyUpdaterToInstallation(installDir string) error {
	// Get current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Read current executable
	data, err := os.ReadFile(exePath)
	if err != nil {
		return fmt.Errorf("failed to read updater: %w", err)
	}

	// Write to installation directory as update.exe
	destPath := filepath.Join(installDir, "update.exe")
	if err := os.WriteFile(destPath, data, 0755); err != nil {
		return fmt.Errorf("failed to write updater: %w", err)
	}

	return nil
}

// ------------------------
// PROXIANI DETECTION
// ------------------------
func isProxianiRunning() bool {
	// Check if node.exe is running
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq node.exe", "/FO", "CSV", "/NH")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// If no node.exe processes, Proxiani is not running
	if !strings.Contains(string(output), "node.exe") {
		return false
	}

	// Check if port 1234 is in use by getting PID from netstat
	cmd = exec.Command("netstat", "-ano", "-p", "tcp")
	output, err = cmd.Output()
	if err != nil {
		return false
	}

	// Look for port 1234 listening
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, ":1234") && strings.Contains(line, "LISTENING") {
			return true
		}
	}

	return false
}

func updateWorldFile(worldFilePath string, updatePort bool) error {
	data, err := os.ReadFile(worldFilePath)
	if err != nil {
		return fmt.Errorf("failed to read world file: %w", err)
	}

	content := string(data)

	// Replace toastsoft.net with localhost in the site attribute
	updatedContent := strings.ReplaceAll(content, `site="toastsoft.net"`, `site="localhost"`)

	// Update port to 7788 for MUDMixer if requested
	if updatePort {
		updatedContent = strings.ReplaceAll(updatedContent, `port="1234"`, `port="7788"`)
	}

	// Check if anything was actually changed
	if updatedContent == content {
		return fmt.Errorf("no toastsoft.net references found in world file")
	}

	// Write back to file
	if err := os.WriteFile(worldFilePath, []byte(updatedContent), 0644); err != nil {
		return fmt.Errorf("failed to write world file: %w", err)
	}

	return nil
}

func updateWorldFileForProxiani(worldFilePath string) error {
	return updateWorldFile(worldFilePath, false)
}

// ------------------------
// MUDMIXER DETECTION
// ------------------------
func isMUDMixerRunning() bool {
	// Check if port 7788 is in use by getting info from netstat
	cmd := exec.Command("netstat", "-ano", "-p", "tcp")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// Look for port 7788 listening
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, ":7788") && strings.Contains(line, "LISTENING") {
			return true
		}
	}

	return false
}

func updateWorldFileForMUDMixer(worldFilePath string) error {
	return updateWorldFile(worldFilePath, true)
}

func isInstalled() bool {
	baseDir, err := os.Getwd()
	if err != nil {
		return false
	}

	// Check if current directory has the key files for an installation
	// We check for actual game files, NOT just .manifest (which can be deleted/corrupted)
	hasMUSHclient := false
	hasWorlds := false
	hasManifest := false

	// Check for MUSHclient.exe (case-insensitive)
	if _, err := os.Stat(filepath.Join(baseDir, "MUSHclient.exe")); err == nil {
		hasMUSHclient = true
	} else if _, err := os.Stat(filepath.Join(baseDir, "mushclient.exe")); err == nil {
		hasMUSHclient = true
	}

	if info, err := os.Stat(filepath.Join(baseDir, "worlds")); err == nil && info.IsDir() {
		hasWorlds = true
	}

	if _, err := os.Stat(filepath.Join(baseDir, manifestFile)); err == nil {
		hasManifest = true
	}

	// If we have MUSHclient.exe AND worlds directory, it's installed
	// OR if we have MUSHclient.exe AND .manifest, it's installed
	// .manifest is for tracking updates
	if hasMUSHclient && (hasWorlds || hasManifest) {
		return true
	}

	return false
}

func hasWorldFilesInCurrentDir() bool {
	baseDir, err := os.Getwd()
	if err != nil {
		return false
	}

	// Check for MUSHclient.exe as a primary indicator of installation
	mushclientPath := filepath.Join(baseDir, "MUSHclient.exe")
	if _, err := os.Stat(mushclientPath); err == nil {
		return true
	}

	worldsDir := filepath.Join(baseDir, "worlds")
	// Check if worlds directory exists
	if info, err := os.Stat(worldsDir); err != nil || !info.IsDir() {
		return false
	}

	// Check if there are any .mcl files in worlds directory
	entries, err := os.ReadDir(worldsDir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".mcl") {
			return true
		}
	}

	return false
}

func needsToUpdateMUSHClientExe(updates []FileInfo) bool {
	for _, file := range updates {
		if strings.ToLower(file.Name) == "mushclient.exe" {
			return true
		}
	}
	return false
}

func isMUSHClientRunning() bool {
	baseDir, err := os.Getwd()
	if err != nil {
		return false
	}

	// Get the expected full path to MUSHclient.exe in this directory
	expectedPath := filepath.Join(baseDir, "MUSHclient.exe")
	expectedPath = strings.ToLower(filepath.Clean(expectedPath))

	// Use WMIC to get all running MUSHclient.exe processes with their full paths
	cmd := exec.Command("wmic", "process", "where", "name='MUSHclient.exe'", "get", "ExecutablePath", "/format:list")
	output, err := cmd.Output()
	if err != nil {
		// WMIC might fail if no processes found or other errors
		return false
	}

	// Parse output - format is "ExecutablePath=C:\path\to\MUSHclient.exe"
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ExecutablePath=") {
			processPath := strings.TrimPrefix(line, "ExecutablePath=")
			processPath = strings.ToLower(filepath.Clean(processPath))

			// Check if this MUSHclient.exe is running from our directory
			if processPath == expectedPath {
				return true
			}
		}
	}

	return false
}

func launchMUSHClient() error {
	baseDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	exePath := filepath.Join(baseDir, "MUSHclient.exe")
	if _, err := os.Stat(exePath); err != nil {
		return fmt.Errorf("MUSHclient.exe not found: %w", err)
	}

	if err := exec.Command(exePath).Start(); err != nil {
		return fmt.Errorf("failed to launch MUSHclient: %w", err)
	}

	return nil
}

// ------------------------
// EXCLUDES
// ------------------------
func loadExcludes() map[string]struct{} {
	baseDir, err := os.Getwd()
	if err != nil {
		return make(map[string]struct{})
	}
	excludes := make(map[string]struct{})
	data, err := os.ReadFile(filepath.Join(baseDir, excludesFile))
	if err != nil {
		return excludes
	}
	lines := strings.Split(string(data), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			// Normalize path for case-insensitive comparison
			normalized := strings.ToLower(normalizePath(l))
			excludes[normalized] = struct{}{}
		}
	}
	return excludes
}

// Supports wildcards like "worlds/*.mcl"
func matchesExclusionPattern(path string, excludes map[string]struct{}) bool {
	normalizedPath := strings.ToLower(normalizePath(path))

	for pattern := range excludes {
		// Check for exact match first
		if normalizedPath == pattern {
			return true
		}

		// Check for wildcard patterns
		if strings.Contains(pattern, "*") {
			matched, _ := filepath.Match(pattern, normalizedPath)
			if matched {
				return true
			}
		}

		// Check for directory prefix (e.g., "worlds/" matches "worlds/myfile.mcl")
		if strings.HasSuffix(pattern, "/") && strings.HasPrefix(normalizedPath, pattern) {
			return true
		}
	}

	return false
}

// ------------------------
// BATCH FILE GENERATION
// ------------------------
func createChannelSwitchBatchFiles(installDir string) error {
	// Create switch-to-stable.bat
	stableBat := filepath.Join(installDir, "Switch to Stable.bat")
	stableContent := "@echo off\nupdate.exe switch stable\n"
	if err := os.WriteFile(stableBat, []byte(stableContent), 0644); err != nil {
		return fmt.Errorf("failed to create switch-to-stable.bat: %w", err)
	}

	// Create Switch to Dev.bat
	devBat := filepath.Join(installDir, "Switch to Dev.bat")
	devContent := "@echo off\nupdate.exe switch dev\n"
	if err := os.WriteFile(devBat, []byte(devContent), 0644); err != nil {
		return fmt.Errorf("failed to create switch-to-dev.bat: %w", err)
	}

	anyBat := filepath.Join(installDir, "Switch to Any Channel.bat")
	anyContent := "@echo off\nupdate.exe switch\n"
	if err := os.WriteFile(anyBat, []byte(anyContent), 0644); err != nil {
		return fmt.Errorf("failed to create switch-to-dev.bat: %w", err)
	}

	return nil
}

// ------------------------
// UTILITIES
// ------------------------

// fatalError shows an error, plays a sound, and waits for user to acknowledge in interactive mode
func fatalError(format string, args ...interface{}) {
	// Play error sound to notify user
	playSoundAsync(errorSound, 0.0)

	// Display the error message
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	} else {
		fmt.Fprintln(os.Stderr, format)
	}

	// In interactive mode, wait for user to press Enter
	if !nonInteractive {
		waitForUser("\nPress Enter to exit...")
	}

	os.Exit(1)
}

// moveToOldFolder moves a file to the .old directory instead of deleting it
func moveToOldFolder(filePath string, relativePath string) error {
	baseDir, err := os.Getwd()
	if err != nil {
		return err
	}

	// Create .old directory if it doesn't exist
	oldDir := filepath.Join(baseDir, ".old")
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		return err
	}

	// Create subdirectories in .old if needed
	oldFilePath := filepath.Join(oldDir, denormalizePath(relativePath))
	if err := os.MkdirAll(filepath.Dir(oldFilePath), 0755); err != nil {
		return err
	}

	// Move the file
	return os.Rename(filePath, oldFilePath)
}

func cleanOldFolder() error {
	baseDir, err := os.Getwd()
	if err != nil {
		return err
	}

	oldDir := filepath.Join(baseDir, ".old")
	if _, err := os.Stat(oldDir); err == nil {
		if !quietFlag && verboseFlag {
			fmt.Println("Cleaning up .old directory from previous run...")
		}
		return os.RemoveAll(oldDir)
	}
	return nil
}

func createUpdaterExcludes() error {
	baseDir, err := os.Getwd()
	if err != nil {
		return err
	}

	var content strings.Builder
	content.WriteString("# Updater Exclusions\n")
	content.WriteString("# This file lists paths that the updater will NEVER touch.\n")
	content.WriteString("# These are typically user configuration files and data.\n")
	content.WriteString("#\n")
	content.WriteString("# Lines starting with # are comments.\n")
	content.WriteString("# One path per line.\n")
	content.WriteString("# Paths are relative to the installation directory.\n")
	content.WriteString("#\n")
	content.WriteString("# DO NOT delete this file unless you want the updater to\n")
	content.WriteString("# potentially overwrite your configuration!\n")
	content.WriteString("\n")
	content.WriteString("# MUSHclient configuration files\n")
	content.WriteString("mushclient.ini\n")
	content.WriteString("mushclient_prefs.sqlite\n")
	content.WriteString("\n")
	content.WriteString("# World configuration files (*.mcl files in worlds directory)\n")
	content.WriteString("worlds/*.mcl\n")
	content.WriteString("\n")

	excludesPath := filepath.Join(baseDir, excludesFile)
	return os.WriteFile(excludesPath, []byte(content.String()), 0644)
}

// ------------------------
// ------------------------

// buildChangelog creates the changelog content string
func buildChangelog(updates []FileInfo, deletedFiles []string) string {
	var changelog strings.Builder
	totalChanges := len(updates) + len(deletedFiles)

	changelog.WriteString("Miriani-Next Update Changelog\n\n")
	changelog.WriteString(fmt.Sprintf("Channel: %s\n", channelFlag))
	changelog.WriteString(fmt.Sprintf("Update completed: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	changelog.WriteString(fmt.Sprintf("Total changes: %d files (%d updated, %d deleted)\n", totalChanges, len(updates), len(deletedFiles)))

	// Add cliff notes for dev/experimental or changelog.txt for stable
	if channelFlag == "stable" {
		// For stable, try to include docs/changelog.txt
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
	} else {
		// For dev/experimental, generate cliff notes from commits
		if commits, err := getCommitsSinceLastUpdate(); err == nil && len(commits) > 0 {
			cliffNotes := generateCliffNotes(commits)
			if cliffNotes != "" {
				changelog.WriteString("\n")
				changelog.WriteString(cliffNotes)
				changelog.WriteString("\n")
			}
		}
	}

	// Add file list at the end
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

// showChangelog displays updated and deleted files and offers to open in notepad
func showChangelog(updates []FileInfo, deletedFiles []string) {
	totalChanges := len(updates) + len(deletedFiles)
	fmt.Printf("\n%d files were changed (%d updated, %d deleted)\n", totalChanges, len(updates), len(deletedFiles))

	// Build the changelog content
	changelogContent := buildChangelog(updates, deletedFiles)

	// Ask if user wants to view changelog
	if !nonInteractive && confirmAction("Would you like to view the detailed changelog?") {
		// Write to temp file
		tmpFile := filepath.Join(os.TempDir(), "next-changelog.txt")
		if err := os.WriteFile(tmpFile, []byte(changelogContent), 0644); err == nil {
			// Open with notepad
			exec.Command("notepad.exe", tmpFile).Start()
		}
	}
}

// waitForUser prompts the user to press Enter to continue
func waitForUser(prompt string) {
	if nonInteractive {
		return
	}
	fmt.Print(prompt)
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

// promptForInstallFolder shows a folder selection dialog using Windows COM APIs
func promptForInstallFolder(defaultPath string) (string, error) {
	if nonInteractive {
		return defaultPath, nil
	}

	// Prompt user to press Enter before opening dialog
	fmt.Println("\nPress Enter to select installation folder...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')

	consoleHandle := getConsoleWindow()

	ole.CoInitialize(0)
	defer ole.CoUninitialize()

	unknown, err := oleutil.CreateObject("Shell.Application")
	if err != nil {
		return "", fmt.Errorf("failed to create Shell object: %w", err)
	}
	defer unknown.Release()

	shell, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return "", fmt.Errorf("failed to get IDispatch interface: %w", err)
	}
	defer shell.Release()

	// Options: 0x10 = BIF_NEWDIALOGSTYLE (modern dialog with "Make New Folder" button)
	folderObj, err := oleutil.CallMethod(shell, "BrowseForFolder", int(consoleHandle),
		"Select installation folder for Miriani-Next", 0x10)
	if err != nil {
		return "", fmt.Errorf("failed to show folder dialog: %w", err)
	}

	if folderObj.Value() == nil {
		return "", fmt.Errorf("folder selection cancelled")
	}

	folderItem := folderObj.ToIDispatch()
	if folderItem == nil {
		return "", fmt.Errorf("folder selection cancelled")
	}
	defer folderItem.Release()

	// Get the Self property (returns FolderItem)
	selfProp, err := oleutil.GetProperty(folderItem, "Self")
	if err != nil {
		return "", fmt.Errorf("failed to get folder item: %w", err)
	}

	selfDispatch := selfProp.ToIDispatch()
	defer selfDispatch.Release()

	// Get the Path property
	pathProp, err := oleutil.GetProperty(selfDispatch, "Path")
	if err != nil {
		return "", fmt.Errorf("failed to get folder path: %w", err)
	}

	selectedPath := pathProp.ToString()
	if selectedPath == "" {
		return "", fmt.Errorf("no folder selected")
	}

	return selectedPath, nil
}

// confirmAction asks the user to confirm an action
func confirmAction(prompt string) bool {
	// In non-interactive mode, always proceed
	if nonInteractive {
		return true
	}

	fmt.Printf("%s (y/n): ", prompt)
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	response = strings.TrimSpace(strings.ToLower(response))
	confirmed := response == "y" || response == "yes"

	// Play select sound first for any valid response
	if confirmed || response == "n" || response == "no" {
		playSound(selectSound)
	}

	// Play success sound when user confirms
	if confirmed {
		playSound(successSound)
	}

	return confirmed
}


// Branch represents a GitHub branch
type Branch struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
		URL string `json:"url"`
	} `json:"commit"`
}

func getBranches() ([]Branch, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/branches", githubOwner, githubRepo)

	resp, err := httpClient.Get(url)
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

// saveChannel saves the selected channel to .update-channel file
func saveChannel(channel string) error {
	baseDir, err := os.Getwd()
	if err != nil {
		return err
	}
	channelPath := filepath.Join(baseDir, channelFile)
	return os.WriteFile(channelPath, []byte(channel), 0644)
}

// loadChannel loads the saved channel from .update-channel file
func loadChannel() (string, error) {
	baseDir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	channelPath := filepath.Join(baseDir, channelFile)
	data, err := os.ReadFile(channelPath)
	if err != nil {
		return "", err
	}
	channel := strings.TrimSpace(string(data))
	return channel, nil
}

func isValidChannel(channel string) bool {
	// Always allow stable and dev
	if channel == "stable" || channel == "dev" {
		return true
	}

	// Check if it's a valid branch name
	branches, err := getBranches()
	if err != nil {
		// If we can't fetch branches, only allow stable/dev
		return false
	}

	for _, branch := range branches {
		if branch.Name == channel {
			return true
		}
	}

	return false
}

// promptInstallationMenu displays an interactive menu for installation options
func promptInstallationMenu(existingInstallFound bool, detectedPath string, toastushPath string) string {
	fmt.Println("\nMiriani-Next Installation")
	fmt.Println()

	if existingInstallFound {
		fmt.Printf("Detected existing installation at: %s\n", detectedPath)
		fmt.Println()
	}

	if toastushPath != "" {
		fmt.Printf("Detected Toastush installation at: %s\n", toastushPath)
		fmt.Println()
	}

	fmt.Println("  1. Install")
	fmt.Println("     Full installation of Miriani-Next")
	fmt.Println()
	fmt.Println("  2. Install Updater")
	fmt.Println("     Add the updater to an existing Miriani-Next installation")
	fmt.Println()
	fmt.Println("  3. Migrate from Toastush")
	fmt.Println("     Upgrade existing Toastush installation to Miriani-Next")
	fmt.Println()
	fmt.Print("Enter your choice (1, 2, or 3): ")

	reader := bufio.NewReader(os.Stdin)
	for {
		response, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("\nError reading input, cancelling installation.")
			return ""
		}

		response = strings.TrimSpace(response)
		switch response {
		case "1":
playSoundAsync(selectSound, 0.0)
			return "1"
		case "2":
			playSoundAsync(selectSound, 0.0)
			return "2"
		case "3":
			playSoundAsync(selectSound, 0.0)
			return "3"
		default:
			fmt.Print("Invalid choice. Please enter 1, 2, or 3: ")
		}
	}
}

// promptForChannel displays an interactive menu to select update channel
func promptForChannel() string {
	fmt.Println("\nMiriani-Next Update Channel Selection")
	fmt.Println()
	fmt.Println("Update channels control how often you receive updates:")
	fmt.Println()

	// Get last update times for stable and dev
	stableDate := ""
	devDate := ""

	if tag, err := getLatestTag(); err == nil {
		if date, err := getLastCommitDate(tag); err == nil {
			stableDate = fmt.Sprintf(" (Last updated: %s)", date)
		}
	}

	if date, err := getLastCommitDate("main"); err == nil {
		devDate = fmt.Sprintf(" (Last updated: %s)", date)
	}

	fmt.Printf("  1. Stable%s\n", stableDate)
	fmt.Println("     Tested, stable releases only")
	fmt.Println("     Updates less frequently but very reliable")
	fmt.Println("     Recommended for most users")
	fmt.Println()
	fmt.Printf("  2. Dev%s\n", devDate)
	fmt.Println("     Latest features and bug fixes")
	fmt.Println("     Updates frequently with new changes")
	fmt.Println("     May occasionally have bugs")
	fmt.Println()
	fmt.Println("  3. Other")
	fmt.Println("     Follow a specific experimental branch")
	fmt.Println("     For advanced users and testing only")
	fmt.Println()
	fmt.Print("Enter your choice (1, 2, or 3): ")

	reader := bufio.NewReader(os.Stdin)
	for {
		response, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("\nError reading input, defaulting to stable.")
			return "stable"
		}

		response = strings.TrimSpace(response)
		switch response {
		case "1":
			playSound(selectSound)
			playSoundAsync(successSound, 0.0)
			fmt.Println("\nUsing the stable channel.")
			return "stable"
		case "2":
			playSound(selectSound)
			playSoundAsync(successSound, 0.0)
			fmt.Println("\nUsing the dev channel.")
			return "dev"
		case "3":
			playSound(selectSound)
			playSoundAsync(successSound, 0.0)
			return promptForBranch()
		default:
			fmt.Print("Invalid choice. Please enter 1, 2, or 3: ")
		}
	}
}

func promptForBranch() string {
	fmt.Println("\nExperimental Branch Selection")
	fmt.Println()
	fmt.Println("Fetching available branches...")

	branches, err := getBranches()
	if err != nil {
		fmt.Printf("Error fetching branches: %v\n", err)
		return promptForChannel()
	}

	// Filter out main (that's "dev") and show others
	var experimentalBranches []Branch
	for _, branch := range branches {
		if branch.Name != "main" {
			experimentalBranches = append(experimentalBranches, branch)
		}
	}

	if len(experimentalBranches) == 0 {
		fmt.Println("No experimental branches available.")
		return promptForChannel()
	}

	fmt.Println("\nAvailable experimental branches:")
	fmt.Println()
	for i, branch := range experimentalBranches {
		fmt.Printf("  %d. %s (commit: %s)\n", i+1, branch.Name, branch.Commit.SHA[:7])
	}
	fmt.Println()
	fmt.Printf("Enter choice (1-%d) or 0 to go back: ", len(experimentalBranches))

	reader := bufio.NewReader(os.Stdin)
	for {
		response, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("\nError reading input, returning to main menu.")
			return promptForChannel()
		}

		response = strings.TrimSpace(response)
		if response == "0" {
			return promptForChannel()
		}

		choice := 0
		fmt.Sscanf(response, "%d", &choice)
		if choice >= 1 && choice <= len(experimentalBranches) {
			selectedBranch := experimentalBranches[choice-1].Name
			playSound(selectSound)
			playSoundAsync(successSound, 0.0)
			fmt.Printf("\nSelected branch: %s\n", selectedBranch)
			fmt.Println("\nWARNING: Experimental branches may be unstable!")
			fmt.Println("Only use this if you know what you're doing.")
			return selectedBranch
		} else {
			fmt.Printf("Invalid choice. Please enter 0-%d: ", len(experimentalBranches))
		}
	}
}

func getLatestVersion() (*Version, error) {
	var version Version

	if channelFlag == "stable" {
		// For stable, get latest tag and parse version from it
		tag, err := getLatestTag()
		if err != nil {
			return nil, fmt.Errorf("failed to get latest tag: %w", err)
		}

		// Parse version from tag (e.g., "v1.2.3" -> 1.2.3)
		tagVersion := strings.TrimPrefix(tag, "v")
		parts := strings.Split(tagVersion, ".")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid tag format: %s (expected vX.Y.Z)", tag)
		}

		major, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid major version in tag %s: %w", tag, err)
		}
		minor, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid minor version in tag %s: %w", tag, err)
		}
		patch, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("invalid patch version in tag %s: %w", tag, err)
		}

		version.Major = major
		version.Minor = minor
		version.Patch = patch
		version.Commit = "" // Stable releases don't have commit SHA in version
	} else {
		// For dev/experimental, get version from latest tag but include commit SHA
		// First, try to get the latest tag to extract version numbers
		tag, err := getLatestTag()
		if err != nil {
			// If we can't get the tag, fall back to 0.0.0
			version.Major = 0
			version.Minor = 0
			version.Patch = 0
		} else {
			// Parse version from tag (e.g., "v1.2.3" -> 1.2.3)
			tagVersion := strings.TrimPrefix(tag, "v")
			parts := strings.Split(tagVersion, ".")
			if len(parts) == 3 {
				major, err1 := strconv.Atoi(parts[0])
				minor, err2 := strconv.Atoi(parts[1])
				patch, err3 := strconv.Atoi(parts[2])
				if err1 == nil && err2 == nil && err3 == nil {
					version.Major = major
					version.Minor = minor
					version.Patch = patch
				} else {
					// Fall back to 0.0.0 if parsing fails
					version.Major = 0
					version.Minor = 0
					version.Patch = 0
				}
			} else {
				// Fall back to 0.0.0 if tag format is unexpected
				version.Major = 0
				version.Minor = 0
				version.Patch = 0
			}
		}

		// Get the commit SHA for the branch
		ref := channelFlag
		if channelFlag == "dev" {
			ref = "main"
		}

		tree, err := getGitHubTree(ref)
		if err != nil {
			return nil, fmt.Errorf("failed to get commit SHA: %w", err)
		}

		// Store first 16 characters of commit SHA
		if len(tree.SHA) >= 16 {
			version.Commit = tree.SHA[:16]
		} else {
			version.Commit = tree.SHA
		}

		if !quietFlag && verboseFlag {
			fmt.Printf("Dev channel version: %d.%d.%d+%s\n", version.Major, version.Minor, version.Patch, version.Commit)
		}
	}

	return &version, nil
}

func getLocalVersion() (*Version, error) {
	baseDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	path := filepath.Join(baseDir, versionFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read local version: %w", err)
	}

	var version Version
	if err := json.Unmarshal(data, &version); err != nil {
		return nil, fmt.Errorf("failed to parse local version: %w", err)
	}

	return &version, nil
}


// detectToastushInstallation attempts to find an existing Toastush installation
func detectToastushInstallation() string {
	// Check Documents folder
	usr, err := os.UserHomeDir()
	if err == nil {
		toastushDir := filepath.Join(usr, "Documents", "Toastush")
		if _, err := os.Stat(filepath.Join(toastushDir, "MUSHclient.exe")); err == nil {
			return toastushDir
		}
	}

	// Check desktop shortcuts
	if path := checkDesktopShortcut("Toastush"); path != "" {
		return path
	}

	return ""
}

// checkDesktopShortcut checks for a desktop shortcut and returns its target path
func checkDesktopShortcut(name string) string {
	userProfile := os.Getenv("USERPROFILE")
	if userProfile == "" {
		return ""
	}

	desktops := []string{
		filepath.Join(userProfile, "Desktop"),
		filepath.Join(userProfile, "OneDrive", "Desktop"),
	}

	for _, desktop := range desktops {
		linkPath := filepath.Join(desktop, name+".lnk")
		if _, err := os.Stat(linkPath); err == nil {
			// Try to read the shortcut target
			if target := getShortcutTarget(linkPath); target != "" {
				// Get the directory containing the target
				targetDir := filepath.Dir(target)
				// Verify it has MUSHclient.exe
				if _, err := os.Stat(filepath.Join(targetDir, "MUSHclient.exe")); err == nil {
					return targetDir
				}
			}
		}
	}

	return ""
}

// getShortcutTarget reads the target path from a Windows shortcut (.lnk file)
func getShortcutTarget(linkPath string) string {
	if err := ole.CoInitialize(0); err != nil {
		return ""
	}
	defer ole.CoUninitialize()

	unknown, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return ""
	}
	defer unknown.Release()

	shell, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return ""
	}
	defer shell.Release()

	link, err := oleutil.CallMethod(shell, "CreateShortcut", linkPath)
	if err != nil {
		return ""
	}

	linkDisp := link.ToIDispatch()
	defer linkDisp.Release()

	target, err := oleutil.GetProperty(linkDisp, "TargetPath")
	if err != nil {
		return ""
	}

	return target.ToString()
}

// handleToastushMigration migrates a Toastush installation to Miriani-Next
func handleToastushMigration(toastushDir string) error {
	// If we didn't auto-detect an installation, prompt for the directory
	if toastushDir == "" {
		if !nonInteractive {
			fmt.Println("\nLocate your Toastush installation directory")
			selectedDir, err := promptForInstallFolder(filepath.Join(os.Getenv("USERPROFILE"), "Documents"))
			if err != nil {
				return fmt.Errorf("error selecting folder: %w", err)
			}
			toastushDir = selectedDir

			// Verify it's a valid installation
			if _, err := os.Stat(filepath.Join(toastushDir, "MUSHclient.exe")); os.IsNotExist(err) {
				return fmt.Errorf("MUSHclient.exe not found in: %s", toastushDir)
			}
		} else {
			return fmt.Errorf("no Toastush installation found and cannot prompt in non-interactive mode")
		}
	} else {
		// Auto-detected installation - confirm with user
		if !nonInteractive {
			fmt.Printf("\nFound Toastush installation at: %s\n", toastushDir)
			if !confirmAction("Migrate this installation?") {
				fmt.Println("\nLocate your Toastush installation directory")
				selectedDir, err := promptForInstallFolder(filepath.Join(os.Getenv("USERPROFILE"), "Documents"))
				if err != nil {
					return fmt.Errorf("error selecting folder: %w", err)
				}
				toastushDir = selectedDir

				// Verify it's a valid installation
				if _, err := os.Stat(filepath.Join(toastushDir, "MUSHclient.exe")); os.IsNotExist(err) {
					return fmt.Errorf("MUSHclient.exe not found in: %s", toastushDir)
				}
			}
		}
	}

	fmt.Printf("\nMigrating Toastush installation from: %s\n", toastushDir)

	// Check if MUSHclient is running before we do anything
	if isMUSHClientRunning() {
		if nonInteractive {
			logProgress("MUSHclient is running. Killing MUSHclient before migration...")
			if err := exec.Command("taskkill", "/IM", "MUSHclient.exe", "/F").Run(); err != nil {
				return fmt.Errorf("failed to kill MUSHclient: %w", err)
			}
			logProgress("MUSHclient killed successfully")
			time.Sleep(1 * time.Second)
		} else {
			fmt.Println("\nMUSHclient is currently running and will be closed to proceed with migration.")
			if confirmAction("Kill MUSHclient and continue?") {
				fmt.Println("Closing MUSHclient...")
				if err := exec.Command("taskkill", "/IM", "MUSHclient.exe", "/F").Run(); err != nil {
					fmt.Printf("Error closing MUSHclient: %v\n", err)
					fmt.Println("Please close MUSHclient manually before proceeding.")
					playSound(errorSound)
					waitForUser("\nPress Enter to exit...")
					return fmt.Errorf("failed to close MUSHclient: %w", err)
				}
				fmt.Println("MUSHclient closed successfully.")
				time.Sleep(1 * time.Second)
			} else {
				fmt.Println("Migration cancelled. Please close MUSHclient and run the migration again.")
				return ErrUserCancelled
			}
		}
	}

	// If no channel was explicitly set, prompt for selection
	if !channelExplicitlySet && !nonInteractive {
		channelFlag = promptForChannel()
	}

	// Check if miriani.mcl has been modified from default
	worldFile := filepath.Join(toastushDir, "worlds", "miriani.mcl")
	mclModified := false
	if hash, err := hashFile(worldFile); err == nil {
		if hash != defaultToastushMCLHash {
			mclModified = true
		}
	}

	// Warn about miriani.mcl if it's been modified
	if mclModified && !nonInteractive {
		fmt.Println("\nWARNING: Modifications detected in miriani.mcl")
		fmt.Println("The installer will replace this file.")
		fmt.Println("This may result in loss of custom connection details or world names/configurations.")
		fmt.Println()
		fmt.Println("NOTE: Miriani-Next has an entirely different configuration system.")
		fmt.Println("Settings in toastush:config will NOT be migrated.")
		fmt.Println()
		if !confirmAction("Continue with migration?") {
			return ErrUserCancelled
		}
	}

	if !quietFlag {
		fmt.Printf("\nInstalling Miriani-Next files to: %s\n", toastushDir)
	}

	// Get the appropriate zipball
	zipURL, err := getZipURLForChannel()
	if err != nil {
		return err
	}

	// Download and extract (as fresh install to replace all files, no file filter = extract all)
	if err := downloadAndExtractZip(zipURL, toastushDir, true, nil); err != nil {
		return fmt.Errorf("failed to download Miriani-Next files: %w", err)
	}

	// Rename directory to Miriani-Next
	newDir := filepath.Join(filepath.Dir(toastushDir), "Miriani-Next")
	if toastushDir != newDir {
		// Check if target already exists
		if _, err := os.Stat(newDir); err == nil {
			if !nonInteractive {
				fmt.Printf("\nDirectory already exists: %s\n", newDir)
				if !confirmAction("Remove existing Miriani-Next directory and continue?") {
					return fmt.Errorf("migration cancelled by user")
				}
			}
			// Remove existing directory
			if err := os.RemoveAll(newDir); err != nil {
				return fmt.Errorf("failed to remove existing directory: %w", err)
			}
		}

		if !quietFlag {
			fmt.Printf("\nRenaming directory to: %s\n", newDir)
		}
		if err := os.Rename(toastushDir, newDir); err != nil {
			return fmt.Errorf("failed to rename directory: %w", err)
		}
		toastushDir = newDir
	}

	// Change to installation directory
	if err := os.Chdir(toastushDir); err != nil {
		return fmt.Errorf("failed to change to installation directory: %w", err)
	}

	// Generate manifest
	if err := saveManifest(); err != nil {
		fmt.Printf("Warning: failed to generate manifest: %v\n", err)
	}

	// Save channel preference
	if err := saveChannel(channelFlag); err != nil {
		fmt.Printf("Warning: failed to save channel preference: %v\n", err)
	}

	// Save version.json with the installed version
	if latestVer, err := getLatestVersion(); err == nil {
		if versionData, err := json.MarshalIndent(latestVer, "", "  "); err == nil {
			if err := os.WriteFile(versionFile, versionData, 0644); err != nil {
				fmt.Printf("Warning: failed to save version file: %v\n", err)
			} else if !quietFlag && verboseFlag {
				fmt.Printf("Saved version: %s\n", latestVer.String())
			}
		}
	}

	// Create channel switching batch files
	if err := createChannelSwitchBatchFiles(toastushDir); err != nil {
		fmt.Printf("Warning: failed to create channel switch batch files: %v\n", err)
	}

	// Copy updater to installation
	if err := copyUpdaterToInstallation(toastushDir); err != nil {
		fmt.Printf("Warning: failed to copy updater: %v\n", err)
	}

	// Update desktop shortcut
	if !quietFlag {
		fmt.Println("\nUpdating desktop shortcut...")
	}
	if err := createDesktopIcon(toastushDir); err != nil {
		if !quietFlag {
			fmt.Printf("Warning: failed to update desktop shortcut: %v\n", err)
		}
	} else if !quietFlag {
		fmt.Println("Desktop shortcut updated!")
	}

	if !quietFlag {
		fmt.Println("\nMigration complete!")
		fmt.Println("Location:", toastushDir)
	}

	return nil
}

// hashFile calculates the SHA1 hash of a file
func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha1.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func createDesktopIcon(targetDir string) error {
	if err := ole.CoInitialize(0); err != nil {
		return fmt.Errorf("failed to initialize COM: %w", err)
	}
	defer ole.CoUninitialize()

	unknown, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return fmt.Errorf("failed to create WScript.Shell: %w", err)
	}
	defer unknown.Release()

	shell, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return fmt.Errorf("failed to query shell interface: %w", err)
	}
	defer shell.Release()

	// Get desktop path using SHGetFolderPath approach
	userProfile := os.Getenv("USERPROFILE")
	if userProfile == "" {
		return fmt.Errorf("failed to get user profile directory")
	}
	desktop := filepath.Join(userProfile, "Desktop")

	// Verify desktop directory exists
	if _, err := os.Stat(desktop); os.IsNotExist(err) {
		// Try alternate location
		desktop = filepath.Join(userProfile, "OneDrive", "Desktop")
		if _, err := os.Stat(desktop); os.IsNotExist(err) {
			return fmt.Errorf("desktop directory not found")
		}
	}

	linkPath := filepath.Join(desktop, "Miriani-Next.lnk")

	link, err := oleutil.CallMethod(shell, "CreateShortcut", linkPath)
	if err != nil {
		return fmt.Errorf("failed to create shortcut: %w", err)
	}
	// Don't call link.Clear() - it causes crashes

	linkDisp := link.ToIDispatch()
	defer linkDisp.Release()

	if _, err := oleutil.PutProperty(linkDisp, "TargetPath", filepath.Join(targetDir, "MUSHclient.exe")); err != nil {
		return fmt.Errorf("failed to set shortcut target: %w", err)
	}
	if _, err := oleutil.PutProperty(linkDisp, "WorkingDirectory", targetDir); err != nil {
		return fmt.Errorf("failed to set shortcut working directory: %w", err)
	}
	if _, err := oleutil.PutProperty(linkDisp, "Description", "Launch Miriani-Next"); err != nil {
		return fmt.Errorf("failed to set shortcut description: %w", err)
	}
	if _, err := oleutil.PutProperty(linkDisp, "WindowStyle", 1); err != nil {
		return fmt.Errorf("failed to set shortcut window style: %w", err)
	}
	_, err = oleutil.CallMethod(linkDisp, "Save")
	if err != nil {
		return fmt.Errorf("failed to save shortcut: %w", err)
	}

	return nil
}
