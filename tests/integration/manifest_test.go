package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	createProcessA = kernel32.NewProc("CreateProcessA")
	createPipe     = kernel32.NewProc("CreatePipe")
	closeHandle    = kernel32.NewProc("CloseHandle")
)

const (
	createNoWindow     = 0x08000000
	startfUseStdHandle = 0x00000100
)

type securityAttributes struct {
	Length             uint32
	SecurityDescriptor uintptr
	InheritHandle      int32
}

type startupInfo struct {
	Cb            uint32
	Reserved      *uint16
	Desktop       *uint16
	Title         *uint16
	X             uint32
	Y             uint32
	XSize         uint32
	YSize         uint32
	XCountChars   uint32
	YCountChars   uint32
	FillAttribute uint32
	Flags         uint32
	ShowWindow    uint16
	CbReserved2   uint16
	LpReserved2   *byte
	StdInput      syscall.Handle
	StdOutput     syscall.Handle
	StdError      syscall.Handle
}

type processInformation struct {
	Process   syscall.Handle
	Thread    syscall.Handle
	ProcessId uint32
	ThreadId  uint32
}

// TestManifestPreventsElevation verifies that the built executable can be launched
// with CreateProcessA + CREATE_NO_WINDOW without requiring elevation.
//
// Windows applies installer detection heuristics to executables named "update", "setup",
// "install" etc, causing CreateProcessA to fail with ERROR_ELEVATION_REQUIRED (740).
// Embedding a manifest with requestedExecutionLevel="asInvoker" disables these heuristics.
func TestManifestPreventsElevation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Build the exe fresh to ensure manifest is embedded
	tmpDir := t.TempDir()
	exePath := filepath.Join(tmpDir, "update.exe")

	// Get the module root (where go.mod is)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	// Navigate up to find the module root
	moduleRoot := wd
	for {
		if _, err := os.Stat(filepath.Join(moduleRoot, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(moduleRoot)
		if parent == moduleRoot {
			t.Fatal("could not find go.mod")
		}
		moduleRoot = parent
	}

	// Run go generate to create rsrc.syso
	generateCmd := exec.Command("go", "generate")
	generateCmd.Dir = moduleRoot
	if output, err := generateCmd.CombinedOutput(); err != nil {
		t.Fatalf("go generate failed: %v\n%s", err, output)
	}

	// Build the exe with "update" in the name (triggers Windows heuristics)
	buildCmd := exec.Command("go", "build", "-o", exePath, ".")
	buildCmd.Dir = moduleRoot
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, output)
	}

	// Verify the exe exists
	if _, err := os.Stat(exePath); err != nil {
		t.Fatalf("built exe not found: %v", err)
	}

	// Test launching with CreateProcessA + CREATE_NO_WINDOW
	// This is exactly how MUSHclient's execute.lua launches the updater
	err = testCreateProcessNoWindow(exePath + " --version")
	if err != nil {
		t.Errorf("CreateProcessA with CREATE_NO_WINDOW failed: %v\n"+
			"This indicates the manifest is not properly embedded.\n"+
			"Windows is applying installer detection heuristics to 'update.exe'.", err)
	}
}

func testCreateProcessNoWindow(cmdLine string) error {
	// Create pipe for stdout (mimics execute.lua)
	var sa securityAttributes
	sa.Length = uint32(unsafe.Sizeof(sa))
	sa.InheritHandle = 1

	var hRead, hWrite syscall.Handle
	ret, _, err := createPipe.Call(
		uintptr(unsafe.Pointer(&hRead)),
		uintptr(unsafe.Pointer(&hWrite)),
		uintptr(unsafe.Pointer(&sa)),
		0,
	)
	if ret == 0 {
		return err
	}
	defer closeHandle.Call(uintptr(hRead))
	defer closeHandle.Call(uintptr(hWrite))

	var si startupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = startfUseStdHandle
	si.StdOutput = hWrite
	si.StdError = hWrite

	var pi processInformation

	cmdBytes := append([]byte(cmdLine), 0)

	ret, _, err = createProcessA.Call(
		0,
		uintptr(unsafe.Pointer(&cmdBytes[0])),
		0,
		0,
		1, // bInheritHandles = TRUE
		createNoWindow,
		0,
		0,
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)

	if ret == 0 {
		return err
	}

	closeHandle.Call(uintptr(pi.Process))
	closeHandle.Call(uintptr(pi.Thread))
	return nil
}
