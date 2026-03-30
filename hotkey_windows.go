//go:build windows

package main

import (
	"errors"
	"runtime"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmHotKey    = 0x0312
	wmQuit      = 0x0012
	modControl  = 0x0002
	modNoRepeat = 0x4000
	vkNumPad9   = 0x69
	hotkeyID    = 0x1909
)

type point struct {
	X int32
	Y int32
}

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterHotKey     = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey   = user32.NewProc("UnregisterHotKey")
	procGetMessage         = user32.NewProc("GetMessageW")
	procPostThreadMessage  = user32.NewProc("PostThreadMessageW")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procDispatchMessage    = user32.NewProc("DispatchMessageW")
	procGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
)

func registerSummonHotkey(onPress func()) (func(), error) {
	ready := make(chan error, 1)
	done := make(chan struct{})
	var threadID uint32

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)

		id, _, _ := procGetCurrentThreadID.Call()
		atomic.StoreUint32(&threadID, uint32(id))

		ret, _, _ := procRegisterHotKey.Call(
			0,
			uintptr(hotkeyID),
			uintptr(modControl|modNoRepeat),
			uintptr(vkNumPad9),
		)
		if ret == 0 {
			ready <- errors.New("RegisterHotKey 调用失败")
			return
		}

		ready <- nil
		defer procUnregisterHotKey.Call(0, uintptr(hotkeyID))

		var message msg
		for {
			result, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
			switch int32(result) {
			case -1, 0:
				return
			}

			if message.Message == wmHotKey && message.WParam == uintptr(hotkeyID) {
				go onPress()
				continue
			}

			procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
		}
	}()

	if err := <-ready; err != nil {
		return nil, err
	}

	stop := func() {
		id := atomic.LoadUint32(&threadID)
		if id == 0 {
			return
		}
		procPostThreadMessage.Call(uintptr(id), uintptr(wmQuit), 0, 0)
		<-done
	}

	return stop, nil
}
