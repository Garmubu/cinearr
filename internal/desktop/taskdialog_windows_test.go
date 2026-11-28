//go:build windows

package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// TASKDIALOGCONFIG is 160 bytes on 64-bit Windows and 96 on 32-bit.
func TestTaskDialogLayout(t *testing.T) {
	cfg, button, _ := Confirm{Title: "t", Action: "a"}.build(0)
	want, wantButton := 160, 12
	if unsafe.Sizeof(uintptr(0)) == 4 {
		want, wantButton = 96, 8
	}
	if len(cfg) != want || len(button) != wantButton {
		t.Fatalf("TASKDIALOGCONFIG %d bytes (want %d), TASKDIALOG_BUTTON %d (want %d)", len(cfg), want, len(button), wantButton)
	}
}

// useCommonControls6 activates version 6 of the common controls on the
// current thread, as the manifest of uninstall.exe does for the process,
// before comctl32.dll is loaded.
func useCommonControls6(dir string) bool {
	manifest := filepath.Join(dir, "cc6.manifest")
	_ = os.WriteFile(manifest, []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0"><dependency><dependentAssembly>
<assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
</dependentAssembly></dependency></assembly>`), 0o644)
	src, _ := syscall.UTF16PtrFromString(manifest)
	ctx := struct {
		size       uint32
		flags      uint32
		source     *uint16
		arch, lang uint16
		dir, res   *uint16
		app        *uint16
		module     uintptr
	}{source: src}
	ctx.size = uint32(unsafe.Sizeof(ctx))
	k32 := syscall.NewLazyDLL("kernel32.dll")
	h, _, _ := k32.NewProc("CreateActCtxW").Call(uintptr(unsafe.Pointer(&ctx)))
	if h == 0 || h == ^uintptr(0) {
		return false
	}
	var cookie uintptr
	r, _, _ := k32.NewProc("ActivateActCtx").Call(h, uintptr(unsafe.Pointer(&cookie)))
	return r != 0
}

// The dialog answers with the action and the check box: a callback ticks
// the box and presses the action button as soon as the dialog is created.
// Skipped where no dialog can be shown.
func TestTaskDialogAnswer(t *testing.T) {
	dir := t.TempDir()
	cb := syscall.NewCallback(func(hwnd, msg, wparam, lparam, ref uintptr) uintptr {
		if msg == tdnCreated {
			sendMessage.Call(hwnd, tdmClickVerification, 1, 0)
			sendMessage.Call(hwnd, tdmClickButton, idAction, 0)
		}
		return 0
	})
	type answer struct {
		ok, checked bool
		err         error
	}
	done := make(chan answer, 1)
	go func() {
		// the activation context belongs to the thread that shows the dialog
		runtime.LockOSThread()
		if !useCommonControls6(dir) {
			done <- answer{err: errors.New("common controls 6 not available")}
			return
		}
		ok, checked, err := Confirm{Title: "Cinearr", Instruction: "Test", Action: "Go", CheckBox: "Box"}.show(cb)
		done <- answer{ok, checked, err}
	}()
	select {
	case a := <-done:
		if a.err != nil {
			t.Skipf("no task dialog here: %v", a.err)
		}
		if !a.ok || !a.checked {
			t.Fatalf("ok %v checked %v", a.ok, a.checked)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the dialog did not answer")
	}
}
