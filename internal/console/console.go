package console

import (
	"bufio"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	user32         = syscall.NewLazyDLL("user32.dll")
	attachConsole  = kernel32.NewProc("AttachConsole")
	allocConsole   = kernel32.NewProc("AllocConsole")
	getStdHandle   = kernel32.NewProc("GetStdHandle")
	showWindowProc = user32.NewProc("ShowWindow")
	setFocusProc   = user32.NewProc("SetFocus")
)

const (
	ATTACH_PARENT_PROCESS = ^uint32(0) // -1 as uint32
	STD_INPUT_HANDLE      = ^uint32(0) - 10 + 1
	STD_OUTPUT_HANDLE     = ^uint32(0) - 11 + 1
	STD_ERROR_HANDLE      = ^uint32(0) - 12 + 1
	SW_MAXIMIZE           = 3
)

var (
	attached bool
	quiet    bool
)

// Init configures the console package
func Init(quietMode bool) {
	quiet = quietMode
}

// SetQuiet changes quiet mode at runtime
func SetQuiet(q bool) {
	quiet = q
}

// IsAttached returns whether a console is attached
func IsAttached() bool {
	return attached
}

// Attach tries to attach to or create a console window.
// Returns true if a console is available for output.
func Attach() bool {
	// Check if we already have a console
	stdOutputHandle, _, _ := getStdHandle.Call(uintptr(STD_OUTPUT_HANDLE))
	if stdOutputHandle != 0 && stdOutputHandle != uintptr(syscall.InvalidHandle) {
		attached = true
		return true
	}

	// Try to attach to parent console
	attachSuccess, _, _ := attachConsole.Call(uintptr(ATTACH_PARENT_PROCESS))

	wasAllocated := false
	if attachSuccess == 0 {
		// No parent console - create a new one
		allocSuccess, _, _ := allocConsole.Call()
		if allocSuccess == 0 {
			return false
		}
		wasAllocated = true
	}

	// Grab handles to stdout, stderr, stdin
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

	// If we created a new console, maximize and focus it
	if wasAllocated {
		hwnd := GetWindow()
		if hwnd != 0 {
			showWindowProc.Call(hwnd, SW_MAXIMIZE)
			showWindowProc.Call(hwnd, 1)
			setFocusProc.Call(hwnd)
		}
	}

	attached = true
	return true
}

// SetTitle sets the console window title
func SetTitle(title string) error {
	if !attached {
		return nil
	}

	lib, err := syscall.LoadLibrary("kernel32.dll")
	if err != nil {
		return err
	}
	defer syscall.FreeLibrary(lib)

	proc, err := syscall.GetProcAddress(lib, "SetConsoleTitleW")
	if err != nil {
		return err
	}

	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return err
	}

	r1, _, err := syscall.Syscall(proc, 1, uintptr(unsafe.Pointer(titlePtr)), 0, 0)
	if r1 == 0 {
		return fmt.Errorf("SetConsoleTitle failed: %v", err)
	}

	return nil
}

// GetWindow returns the console window handle (HWND)
func GetWindow() uintptr {
	lib, err := syscall.LoadLibrary("kernel32.dll")
	if err != nil {
		return 0
	}
	defer syscall.FreeLibrary(lib)

	proc, err := syscall.GetProcAddress(lib, "GetConsoleWindow")
	if err != nil {
		return 0
	}

	hwnd, _, _ := syscall.Syscall(proc, 0, 0, 0, 0)
	return hwnd
}

// WaitForKey prompts the user to press Enter. Does nothing in non-interactive mode.
func WaitForKey(prompt string, nonInteractive bool) {
	if nonInteractive {
		return
	}
	fmt.Print(prompt)
	_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
}

// Log prints a message if not in quiet mode
func Log(format string, args ...interface{}) {
	if !quiet {
		fmt.Printf(format+"\n", args...)
	}
}
