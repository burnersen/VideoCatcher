// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// The icon in the notification area, shown while the window is closed but
// downloads go on. Wails v2 has none of its own, so this is plain Win32: a
// hidden window of its own on a thread of its own receives the clicks.
// Left click opens the window again, the right-click menu has "Open" and
// "Quit".
//
// Calls from other threads go to that window with SendMessage, so the icon
// is only ever changed on its own thread, and the caller learns whether it
// worked (a hidden window without an icon could not be found again).

import (
	"errors"
	"os"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	trayUser32                 = windows.NewLazySystemDLL("user32.dll")
	trayShell32                = windows.NewLazySystemDLL("shell32.dll")
	trayKernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterClassExW       = trayUser32.NewProc("RegisterClassExW")
	procCreateWindowExW        = trayUser32.NewProc("CreateWindowExW")
	procDefWindowProcW         = trayUser32.NewProc("DefWindowProcW")
	procGetMessageW            = trayUser32.NewProc("GetMessageW")
	procTranslateMessage       = trayUser32.NewProc("TranslateMessage")
	procDispatchMessageW       = trayUser32.NewProc("DispatchMessageW")
	procSendMessageW           = trayUser32.NewProc("SendMessageW")
	procPostMessageW           = trayUser32.NewProc("PostMessageW")
	procDestroyWindow          = trayUser32.NewProc("DestroyWindow")
	procPostQuitMessage        = trayUser32.NewProc("PostQuitMessage")
	procCreatePopupMenu        = trayUser32.NewProc("CreatePopupMenu")
	procAppendMenuW            = trayUser32.NewProc("AppendMenuW")
	procTrackPopupMenu         = trayUser32.NewProc("TrackPopupMenu")
	procDestroyMenu            = trayUser32.NewProc("DestroyMenu")
	procSetForegroundWindow    = trayUser32.NewProc("SetForegroundWindow")
	procGetCursorPos           = trayUser32.NewProc("GetCursorPos")
	procRegisterWindowMessageW = trayUser32.NewProc("RegisterWindowMessageW")
	procLoadIconW              = trayUser32.NewProc("LoadIconW")
	procDestroyIcon            = trayUser32.NewProc("DestroyIcon")
	procShellNotifyIconW       = trayShell32.NewProc("Shell_NotifyIconW")
	procExtractIconExW         = trayShell32.NewProc("ExtractIconExW")
	procGetModuleHandleW       = trayKernel32.NewProc("GetModuleHandleW")
)

// Win32 numbers (WinUser.h, shellapi.h).
const (
	wmNull         = 0x0000
	wmDestroy      = 0x0002
	wmClose        = 0x0010
	wmLButtonUp    = 0x0202
	wmRButtonUp    = 0x0205
	wmUser         = 0x0400
	wmTrayCallback = wmUser + 1 // clicks on the icon
	wmTrayShow     = wmUser + 2 // from other threads: show the icon
	wmTrayHide     = wmUser + 3
	wmTrayTip      = wmUser + 4

	nimAdd     = 0
	nimModify  = 1
	nimDelete  = 2
	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10
	niifInfo   = 0x01

	mfString       = 0x0000
	mfSeparator    = 0x0800
	tpmRightBtn    = 0x0002
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100
	idiApplication = 32512

	trayCmdOpen = 1
	trayCmdQuit = 2
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GUIDItem         windows.GUID
	HBalloonIcon     uintptr
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type winMsg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	Private uint32
}

// TrayTexts come from the page, in its language.
type TrayTexts struct {
	Open         string `json:"open"`         // menu: open the window
	Quit         string `json:"quit"`         // menu: quit VideoCatcher
	Running      string `json:"running"`      // tooltip while downloads run, "{n}" is their number
	BalloonTitle string `json:"balloonTitle"` // once per run, when the window goes into the tray
	BalloonText  string `json:"balloonText"`
}

type trayIcon struct {
	mu      sync.Mutex
	hwnd    uintptr
	icon    uintptr
	texts   TrayTexts
	tip     string
	balloon bool // show the balloon with the next Show
	visible bool
	onOpen  func()
	onQuit  func()
	done    chan struct{}
	// TaskbarCreated: Explorer restarted, icons must be added again.
	taskbarCreated uintptr
}

// One tray per process; the window procedure finds it here.
var (
	trayMu      sync.Mutex
	activeTray  *trayIcon
	trayWndProc = windows.NewCallback(trayWindowProc)
)

func newTray(onOpen, onQuit func()) (*trayIcon, error) {
	t := &trayIcon{onOpen: onOpen, onQuit: onQuit, done: make(chan struct{}),
		texts: TrayTexts{Open: "Open " + displayName, Quit: "Quit", Running: displayName + ": {n} downloads"}}
	ready := make(chan error, 1)
	go t.loop(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return t, nil
}

func (t *trayIcon) loop(ready chan<- error) {
	runtime.LockOSThread() // the window belongs to this thread; its messages come here
	defer close(t.done)
	trayMu.Lock()
	if activeTray != nil {
		trayMu.Unlock()
		ready <- errors.New("there is a tray icon already")
		return
	}
	activeTray = t
	trayMu.Unlock()
	defer func() {
		trayMu.Lock()
		activeTray = nil
		trayMu.Unlock()
	}()

	inst, _, _ := procGetModuleHandleW.Call(0)
	class := windows.StringToUTF16Ptr("VideoCatcherTray")
	wc := wndClassEx{WndProc: trayWndProc, Instance: inst, ClassName: class}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && !errors.Is(err, windows.ERROR_CLASS_ALREADY_EXISTS) {
		ready <- err
		return
	}
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(displayName))),
		0, 0, 0, 0, 0, 0, 0, inst, 0)
	if hwnd == 0 {
		ready <- err
		return
	}
	t.hwnd = hwnd
	t.icon = loadAppIcon()
	t.taskbarCreated, _, _ = procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("TaskbarCreated"))))
	ready <- nil

	var m winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	if t.icon != 0 {
		procDestroyIcon.Call(t.icon) // ignore: the process ends or a new tray loads its own
	}
}

// loadAppIcon takes the small icon of VideoCatcher.exe, or the standard
// program icon.
func loadAppIcon() uintptr {
	if exe, err := os.Executable(); err == nil {
		var small uintptr
		if n, _, _ := procExtractIconExW.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(exe))), 0, 0,
			uintptr(unsafe.Pointer(&small)), 1); n > 0 && small != 0 {
			return small
		}
	}
	icon, _, _ := procLoadIconW.Call(0, idiApplication)
	return icon
}

func trayWindowProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	trayMu.Lock()
	t := activeTray
	trayMu.Unlock()
	if t == nil || t.hwnd != hwnd {
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
		return r
	}
	switch {
	case msg == wmTrayShow:
		return boolResult(t.add())
	case msg == wmTrayHide:
		t.remove()
		return 1
	case msg == wmTrayTip:
		t.modifyTip()
		return 1
	case msg == wmTrayCallback:
		switch lparam & 0xffff {
		case wmLButtonUp:
			go t.onOpen()
		case wmRButtonUp:
			t.menu()
		}
		return 0
	case msg == wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case msg == wmDestroy:
		t.remove()
		procPostQuitMessage.Call(0)
		return 0
	case t.taskbarCreated != 0 && msg == t.taskbarCreated:
		t.mu.Lock()
		again := t.visible
		t.visible = false
		t.mu.Unlock()
		if again {
			t.add() // ignore: if Explorer is not ready, the window can still be opened by starting VideoCatcher again
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func boolResult(err error) uintptr {
	if err != nil {
		return 0
	}
	return 1
}

// data fills the structure for Shell_NotifyIcon; t.mu is held.
func (t *trayIcon) dataLocked(flags uint32) notifyIconData {
	d := notifyIconData{HWnd: t.hwnd, UID: 1, UFlags: flags, UCallbackMessage: wmTrayCallback, HIcon: t.icon}
	d.CbSize = uint32(unsafe.Sizeof(d))
	putUTF16(d.SzTip[:], t.tip)
	return d
}

// putUTF16 copies s into a fixed field, cut to fit, always ending in NUL.
func putUTF16(dst []uint16, s string) {
	u := windows.StringToUTF16(s)
	if len(u) > len(dst) {
		u = append(u[:len(dst)-1], 0)
	}
	copy(dst, u)
}

func (t *trayIcon) add() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.visible {
		return nil
	}
	d := t.dataLocked(nifMessage | nifIcon | nifTip)
	if t.balloon {
		d.UFlags |= nifInfo
		d.DwInfoFlags = niifInfo
		putUTF16(d.SzInfoTitle[:], t.texts.BalloonTitle)
		putUTF16(d.SzInfo[:], t.texts.BalloonText)
	}
	if r, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&d))); r == 0 {
		return err
	}
	t.visible, t.balloon = true, false
	return nil
}

func (t *trayIcon) remove() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.visible {
		return
	}
	d := t.dataLocked(0)
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&d))) // ignore: gone is gone
	t.visible = false
}

func (t *trayIcon) modifyTip() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.visible {
		return
	}
	d := t.dataLocked(nifTip)
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&d))) // ignore: an old tooltip is no harm
}

func (t *trayIcon) menu() {
	t.mu.Lock()
	open := windows.StringToUTF16(t.texts.Open)
	quit := windows.StringToUTF16(t.texts.Quit)
	t.mu.Unlock()
	m, _, _ := procCreatePopupMenu.Call()
	if m == 0 {
		return
	}
	defer procDestroyMenu.Call(m)
	procAppendMenuW.Call(m, mfString, trayCmdOpen, uintptr(unsafe.Pointer(&open[0])))
	procAppendMenuW.Call(m, mfSeparator, 0, 0)
	procAppendMenuW.Call(m, mfString, trayCmdQuit, uintptr(unsafe.Pointer(&quit[0])))
	var pt struct{ X, Y int32 }
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Without this the menu stays open when the user clicks elsewhere.
	procSetForegroundWindow.Call(t.hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(m, tpmReturnCmd|tpmNoNotify|tpmRightBtn, uintptr(pt.X), uintptr(pt.Y), 0, t.hwnd, 0)
	procPostMessageW.Call(t.hwnd, wmNull, 0, 0)
	runtime.KeepAlive(open)
	runtime.KeepAlive(quit)
	switch cmd {
	case trayCmdOpen:
		go t.onOpen()
	case trayCmdQuit:
		go t.onQuit()
	}
}

// SetTexts changes the words of menu, tooltip and balloon.
func (t *trayIcon) SetTexts(x TrayTexts) {
	t.mu.Lock()
	t.texts = x
	t.mu.Unlock()
}

// Show puts the icon into the notification area. balloon: tell the user
// once that VideoCatcher goes on there.
func (t *trayIcon) Show(tip string, balloon bool) error {
	t.mu.Lock()
	t.tip, t.balloon = tip, balloon && t.texts.BalloonText != ""
	t.mu.Unlock()
	if r, _, _ := procSendMessageW.Call(t.hwnd, wmTrayShow, 0, 0); r == 0 {
		return errors.New("the notification area did not take the icon")
	}
	return nil
}

func (t *trayIcon) Hide() { procSendMessageW.Call(t.hwnd, wmTrayHide, 0, 0) }

// SetTip changes the tooltip while the icon is shown.
func (t *trayIcon) SetTip(tip string) {
	t.mu.Lock()
	same := t.tip == tip
	t.tip = tip
	visible := t.visible
	t.mu.Unlock()
	if !same && visible {
		procPostMessageW.Call(t.hwnd, wmTrayTip, 0, 0)
	}
}

func (t *trayIcon) Visible() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.visible
}

func (t *trayIcon) Texts() TrayTexts {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.texts
}

// Close removes the icon and ends the thread.
func (t *trayIcon) Close() {
	procPostMessageW.Call(t.hwnd, wmClose, 0, 0)
	<-t.done
}
