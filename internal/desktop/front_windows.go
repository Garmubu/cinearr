//go:build windows

package desktop

import (
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// Windows only lets the process the user is working in (here the browser
// showing Cinearr) put a window in the foreground: a window opened by a
// program Cinearr starts (the player, the Explorer, update.exe) would open
// behind. Once the window appears, the input of the calling thread is
// attached to the one of the foreground window, which lets it hand the
// foreground over; failing that, the window is at least raised above the
// others.

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	procEnumWindows          = user32.NewProc("EnumWindows")
	procGetWindowThreadPID   = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible      = user32.NewProc("IsWindowVisible")
	procIsWindowEnabled      = user32.NewProc("IsWindowEnabled")
	procGetWindow            = user32.NewProc("GetWindow")
	procGetForegroundWindow  = user32.NewProc("GetForegroundWindow")
	procSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop     = user32.NewProc("BringWindowToTop")
	procAttachThreadInput    = user32.NewProc("AttachThreadInput")
	procShowWindow           = user32.NewProc("ShowWindow")
	procIsIconic             = user32.NewProc("IsIconic")
	procSetWindowPos         = user32.NewProc("SetWindowPos")
	procSwitchToThisWindow   = user32.NewProc("SwitchToThisWindow")
	procPeekMessage          = user32.NewProc("PeekMessageW")
	procGetClassName         = user32.NewProc("GetClassNameW")
	procGetCurrentThreadID   = syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentThreadId")
	procAllowSetForeground   = user32.NewProc("AllowSetForegroundWindow")
	enumWindowsCallbackOnce  = syscall.NewCallback(enumWindowsCallback)
	enumWindowsCallbackFound func(hwnd uintptr) bool
)

const (
	gwOwner       = 4
	swRestore     = 9
	hwndTopmost   = ^uintptr(0)     // -1
	hwndNoTopmost = ^uintptr(0) - 1 // -2
	swpNoMove     = 0x0002
	swpNoSize     = 0x0001
	swpShowWindow = 0x0040
)

// enumWindowsCallback calls the function of the running enumeration (the
// callback is created once: Windows limits the number of callbacks).
func enumWindowsCallback(hwnd, _ uintptr) uintptr {
	if enumWindowsCallbackFound != nil && enumWindowsCallbackFound(hwnd) {
		return 0 // stop
	}
	return 1
}

var enumMu = make(chan struct{}, 1)

// topWindows lists the visible top-level windows (owned ones included).
func topWindows() []uintptr {
	enumMu <- struct{}{}
	defer func() { <-enumMu }()
	var out []uintptr
	enumWindowsCallbackFound = func(h uintptr) bool {
		if v, _, _ := procIsWindowVisible.Call(h); v != 0 {
			out = append(out, h)
		}
		return false
	}
	procEnumWindows.Call(enumWindowsCallbackOnce, 0)
	enumWindowsCallbackFound = nil
	return out
}

func windowPID(h uintptr) uint32 {
	var pid uint32
	procGetWindowThreadPID.Call(h, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func windowClass(h uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassName.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

// mainWindow returns the window of the process to bring forward: an
// enabled one (a modal dialog rather than the window it disables), not
// owned if possible.
func mainWindow(pid uint32) uintptr {
	var owned uintptr
	for _, h := range topWindows() {
		if windowPID(h) != pid {
			continue
		}
		if e, _, _ := procIsWindowEnabled.Call(h); e == 0 {
			continue
		}
		if o, _, _ := procGetWindow.Call(h, gwOwner); o == 0 {
			return h
		}
		if owned == 0 {
			owned = h
		}
	}
	return owned
}

// foreground puts hwnd in the foreground, with the keyboard focus.
func foreground(hwnd uintptr) bool {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// AttachThreadInput needs a message queue on this thread
	var msg [64]byte
	procPeekMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, 0)
	if i, _, _ := procIsIconic.Call(hwnd); i != 0 {
		procShowWindow.Call(hwnd, swRestore)
	}
	fg, _, _ := procGetForegroundWindow.Call()
	if fg == hwnd {
		return true
	}
	cur, _, _ := procGetCurrentThreadID.Call()
	fgThread, _, _ := procGetWindowThreadPID.Call(fg, 0)
	attached := false
	if fgThread != 0 && fgThread != cur {
		r, _, _ := procAttachThreadInput.Call(cur, fgThread, 1)
		attached = r != 0
	}
	procBringWindowToTop.Call(hwnd)
	procSetForegroundWindow.Call(hwnd)
	if attached {
		procAttachThreadInput.Call(cur, fgThread, 0)
	}
	if now, _, _ := procGetForegroundWindow.Call(); now == hwnd {
		return true
	}
	procSwitchToThisWindow.Call(hwnd, 1)
	if now, _, _ := procGetForegroundWindow.Call(); now == hwnd {
		return true
	}
	// at least above the other windows, without the focus
	procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShowWindow)
	procSetWindowPos.Call(hwnd, hwndNoTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShowWindow)
	return false
}

// BringToFront waits, in the background, for the window of the process
// pid (started by an action of the user) and brings it to the foreground.
func BringToFront(pid int) {
	if pid <= 0 {
		return
	}
	procAllowSetForeground.Call(uintptr(pid))
	go func() {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if h := mainWindow(uint32(pid)); h != 0 {
				// the window may still be setting itself up
				time.Sleep(150 * time.Millisecond)
				foreground(h)
				return
			}
			time.Sleep(150 * time.Millisecond)
		}
	}()
}

// bringNewWindowToFront brings forward the first window of class that
// appears after the snapshot before (the Explorer opens folders in its
// own process, not in the one Cinearr starts).
func bringNewWindowToFront(class string, before map[uintptr]bool) {
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			for _, h := range topWindows() {
				if !before[h] && windowClass(h) == class {
					time.Sleep(150 * time.Millisecond)
					foreground(h)
					return
				}
			}
			time.Sleep(150 * time.Millisecond)
		}
	}()
}

// windowsOfClass lists the visible windows of a class.
func windowsOfClass(class string) map[uintptr]bool {
	out := map[uintptr]bool{}
	for _, h := range topWindows() {
		if windowClass(h) == class {
			out[h] = true
		}
	}
	return out
}

// openInFront prepares the opening of a folder in the Explorer: the call
// returned brings its new window forward once started.
func openInFront(path string) func() {
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return func() {}
	}
	before := windowsOfClass("CabinetWClass")
	return func() { bringNewWindowToFront("CabinetWClass", before) }
}
