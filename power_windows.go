// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Sleep, hibernate and shut down for "when all downloads are done"
// (afterall.go), and keeping the PC awake while downloads run.

import (
	"runtime"

	"golang.org/x/sys/windows"
)

var (
	procSetSuspendState         = windows.NewLazySystemDLL("powrprof.dll").NewProc("SetSuspendState")
	procSetThreadExecutionState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")
)

// WinBase.h: the state holds until it is taken back; the system must not
// go to sleep by itself. The screen may still turn off.
const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

// awake keeps Windows from going to sleep by itself while downloads run
// (the user can still send the PC to sleep). Windows counts the wish per
// thread, and a goroutine changes threads: one goroutine tied to its
// thread makes and takes back the wish. Windows drops it when the program
// ends, however it ends.
type awake struct {
	want chan bool
}

func newAwake() *awake {
	a := &awake{want: make(chan bool, 1)}
	go func() {
		runtime.LockOSThread()
		on := false
		for w := range a.want {
			if w == on {
				continue
			}
			state := uintptr(esContinuous)
			if w {
				state |= esSystemRequired
			}
			procSetThreadExecutionState.Call(state) // ignore: without it the PC sleeps as it always did
			on = w
		}
	}()
	return a
}

// Set says whether the PC should stay awake from now on. Only the newest
// wish counts; it never blocks.
func (a *awake) Set(on bool) {
	for {
		select {
		case a.want <- on:
			return
		default:
		}
		select {
		case <-a.want: // an older wish nobody took yet
		default:
		}
	}
}

// suspendPC puts the PC to sleep, or into hibernation. Called directly,
// SetSuspendState(FALSE) sleeps also when hibernation is on (the known
// trouble is only with "rundll32 powrprof.dll,SetSuspendState").
func suspendPC(hibernate bool) error {
	h := uintptr(0)
	if hibernate {
		h = 1
	}
	if r, _, err := procSetSuspendState.Call(h, 0, 0); r == 0 {
		return err
	}
	return nil
}

// shutdownPC turns the PC off like the start menu does: programs are asked
// to close (VideoCatcher saves its list then), nothing is forced.
func shutdownPC() error {
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &tok); err != nil {
		return err
	}
	defer tok.Close() // ignore: only a handle
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr("SeShutdownPrivilege"), &luid); err != nil {
		return err
	}
	tp := windows.Tokenprivileges{PrivilegeCount: 1}
	tp.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
	if err := windows.AdjustTokenPrivileges(tok, false, &tp, 0, nil, nil); err != nil {
		return err
	}
	return windows.ExitWindowsEx(windows.EWX_SHUTDOWN|windows.EWX_POWEROFF,
		windows.SHTDN_REASON_MAJOR_APPLICATION|windows.SHTDN_REASON_MINOR_OTHER|windows.SHTDN_REASON_FLAG_PLANNED)
}
