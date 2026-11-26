//go:build windows

package desktop

import (
	"syscall"
	"unsafe"
)

// Answers of Ask (MessageBox results).
const (
	Yes    = 6
	No     = 7
	Cancel = 2
)

const (
	mbYesNo       = 0x4
	mbYesNoCancel = 0x3
	mbIconInfo    = 0x40
	mbIconQuest   = 0x20
	mbForeground  = 0x10000
	mbTopMost     = 0x40000
)

func box(title, msg string, flags uintptr) int {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(msg)
	r, _, _ := messageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags|mbForeground|mbTopMost)
	return int(r)
}

// Info shows an information dialog.
func Info(title, msg string) { box(title, msg, mbIconInfo) }

// Ask asks a yes/no question (yes/no/cancel when withCancel is true) and
// returns Yes, No or Cancel.
func Ask(title, msg string, withCancel bool) int {
	if withCancel {
		return box(title, msg, mbYesNoCancel|mbIconQuest)
	}
	return box(title, msg, mbYesNo|mbIconQuest)
}

var userUILanguage = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

// SystemLanguage returns the language of Windows among those of the
// interface ("fr", "es", "it", "de", or "en").
func SystemLanguage() string {
	r, _, _ := userUILanguage.Call()
	switch r & 0x3ff { // primary language
	case 0x0c:
		return "fr"
	case 0x0a:
		return "es"
	case 0x10:
		return "it"
	case 0x07:
		return "de"
	}
	return "en"
}
