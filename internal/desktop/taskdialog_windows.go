//go:build windows

package desktop

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// Confirm is a Windows task dialog with one action button, a Cancel
// button and a check box: the clear form of a question such as "Uninstall,
// and also delete the data?". It returns whether the action was chosen and
// whether the box was ticked.
type Confirm struct {
	Title       string // window title
	Instruction string // main sentence, in large type
	Content     string // details under it
	Action      string // label of the action button ("Uninstall")
	CheckBox    string // label of the check box
	Checked     bool   // initial state of the check box
}

const (
	tdfAllowDialogCancellation = 0x0008
	tdcbfCancelButton          = 0x0008
	idAction                   = 1001
	idCancel                   = 2
	tdnCreated                 = 0
	wmUser                     = 0x0400
	tdmClickButton             = wmUser + 102
	tdmClickVerification       = wmUser + 113
)

var (
	comctl32       = syscall.NewLazyDLL("comctl32.dll")
	taskDialogProc = comctl32.NewProc("TaskDialogIndirect")
	sendMessage    = syscall.NewLazyDLL("user32.dll").NewProc("SendMessageW")
)

// Show displays the dialog. An error means the task dialog is not
// available (common controls 6 not loaded): the caller asks otherwise.
func (c Confirm) Show() (ok, checked bool, err error) {
	return c.show(0)
}

// show displays the dialog; callback is an optional PFTASKDIALOGCALLBACK
// (used by the tests).
func (c Confirm) show(callback uintptr) (ok, checked bool, err error) {
	if err := taskDialogProc.Find(); err != nil {
		return false, false, err
	}
	cfg, button, keep := c.build(callback)
	var pressed, radio, verified int32
	hr, _, _ := taskDialogProc.Call(uintptr(unsafe.Pointer(&cfg[0])),
		uintptr(unsafe.Pointer(&pressed)), uintptr(unsafe.Pointer(&radio)), uintptr(unsafe.Pointer(&verified)))
	runtime.KeepAlive(cfg)
	runtime.KeepAlive(button)
	runtime.KeepAlive(keep)
	if int32(hr) < 0 {
		return false, false, fmt.Errorf("TaskDialogIndirect: 0x%08x", uint32(hr))
	}
	return pressed == idAction, verified != 0, nil
}

// build lays out TASKDIALOGCONFIG by hand: the structure is declared with
// 1-byte packing in commctrl.h, which a Go struct cannot express. The
// button and the strings it points to are returned to be kept alive.
func (c Confirm) build(callback uintptr) (cfgBytes, button []byte, keep [][]uint16) {
	str := func(s string) uintptr {
		if s == "" {
			return 0
		}
		p, _ := syscall.UTF16FromString(s)
		keep = append(keep, p)
		return uintptr(unsafe.Pointer(&p[0]))
	}
	ptr := int(unsafe.Sizeof(uintptr(0)))

	// TASKDIALOG_BUTTON (packed): int nButtonID; PCWSTR pszButtonText
	button = make([]byte, 4+ptr)
	binary.LittleEndian.PutUint32(button, idAction)
	putPtr(button[4:], str(c.Action), ptr)

	cfg := newPacked(ptr)
	cfg.u32(0) // cbSize, set below
	cfg.ptr(0) // hwndParent
	cfg.ptr(0) // hInstance
	cfg.u32(tdfAllowDialogCancellation)
	cfg.u32(tdcbfCancelButton)
	cfg.ptr(str(c.Title))
	cfg.ptr(0) // hMainIcon
	cfg.ptr(str(c.Instruction))
	cfg.ptr(str(c.Content))
	cfg.u32(1) // cButtons
	cfg.ptr(uintptr(unsafe.Pointer(&button[0])))
	cfg.u32(idAction) // nDefaultButton
	cfg.u32(0)        // cRadioButtons
	cfg.ptr(0)        // pRadioButtons
	cfg.u32(0)        // nDefaultRadioButton
	cfg.ptr(str(c.CheckBox))
	cfg.ptr(0) // pszExpandedInformation
	cfg.ptr(0) // pszExpandedControlText
	cfg.ptr(0) // pszCollapsedControlText
	cfg.ptr(0) // hFooterIcon
	cfg.ptr(0) // pszFooter
	cfg.ptr(callback)
	cfg.ptr(0) // lpCallbackData
	cfg.u32(0) // cxWidth
	binary.LittleEndian.PutUint32(cfg.b, uint32(len(cfg.b)))
	if c.Checked {
		// TDF_VERIFICATION_FLAG_CHECKED
		flags := binary.LittleEndian.Uint32(cfg.b[4+2*ptr:])
		binary.LittleEndian.PutUint32(cfg.b[4+2*ptr:], flags|0x0100)
	}
	return cfg.b, button, keep
}

// packed appends the fields of a 1-byte packed structure.
type packed struct {
	b    []byte
	size int // size of a pointer
}

func newPacked(size int) *packed { return &packed{size: size} }

func (p *packed) u32(v uint32) {
	p.b = binary.LittleEndian.AppendUint32(p.b, v)
}

func (p *packed) ptr(v uintptr) {
	buf := make([]byte, p.size)
	putPtr(buf, v, p.size)
	p.b = append(p.b, buf...)
}

func putPtr(b []byte, v uintptr, size int) {
	if size == 8 {
		binary.LittleEndian.PutUint64(b, uint64(v))
	} else {
		binary.LittleEndian.PutUint32(b, uint32(v))
	}
}
