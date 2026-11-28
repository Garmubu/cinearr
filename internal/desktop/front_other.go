//go:build !windows

package desktop

// BringToFront brings forward the window of a process started by an action
// of the user. On macOS the applications are opened with "open", which
// activates them, and mpv takes the focus by itself (--focus-on=open); on
// Linux the window manager gives the focus to new windows.
func BringToFront(pid int) {}

// openInFront: "open" (macOS) and the file managers of Linux show their new
// window in front by themselves.
func openInFront(path string) func() { return func() {} }
