// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

//go:build windows

// VideoCatcher is a window for Windows. Replace this sentence with one that
// says what it does; the structure stays.
//
// Where things are (template go-wails of dev-starterkit):
//
//	main.go      start of the window; a failed start shows a message box
//	app.go       everything the page may call; small methods, one job each
//	errors.go    UserError and how errors reach the page
//	logging.go   log file with rotation       (same as template go-cli)
//	paths.go     where settings and log live  (same as template go-cli)
//	files.go     atomic writes                (same as template go-cli)
//	frontend/    plain HTML, CSS and JavaScript, no npm, no build step
package main

import (
	"embed"
	"fmt"
	"log/slog"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// singleInstanceID must never change, or a new version would not find an
// older one that is still running. Make a new one per project (any UUID).
const singleInstanceID = "VideoCatcher-33ea85f3-9783-4a35-820b-913c5416f52c"

func main() {
	paths, err := defaultPaths()
	if err != nil {
		showFatal("could not find its folders", err)
		return
	}
	logger := slog.New(slog.DiscardHandler)
	if l, closeLog, err := openLog(paths.LogFile, slog.LevelInfo); err == nil {
		logger = l
		defer closeLog()
	}
	logger.Info("start", "version", version)

	app := NewApp(paths, logger)
	err = wails.Run(&options.App{
		Title:     appName,
		Width:     760,
		Height:    560,
		MinWidth:  520,
		MinHeight: 420,
		// Must match --bg in style.css, or the window flashes white.
		BackgroundColour: &options.RGBA{R: 246, G: 246, B: 248, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               singleInstanceID,
			OnSecondInstanceLaunch: app.onSecondInstance,
		},
		Windows: &windows.Options{
			Theme:    windows.SystemDefault,
			Messages: windowMessages(),
		},
		Bind: []any{app},
	})
	if err != nil {
		logger.Error("window failed", "err", err)
		showFatal("could not open its window", err)
	}
}

// windowMessages replaces Wails' wording for a crash of the Windows web view,
// which reads as if the program itself had crashed. The other messages (for
// example the WebView2 download) stay as Wails has them.
func windowMessages() *windows.Messages {
	m := windows.DefaultMessages()
	m.WebView2ProcessCrash = "The Windows web view (WebView2) stopped. " + appName + " itself did not crash, " +
		"and nothing was changed. Please start it again. If this keeps happening, repair the web view: " +
		"Settings > Apps > Installed apps > Microsoft Edge WebView2 Runtime > Modify > Repair."
	return m
}

// showFatal tells the user why the window cannot start. A window program has
// no console: log.Fatal or a panic here would end it without a word.
func showFatal(what string, err error) {
	text := fmt.Sprintf("%s %s.\n\n%v\n\nPlease report this at https://github.com/burnersen/%s/issues", appName, what, err, appName)
	title, _ := syscall.UTF16PtrFromString(appName) // ignore: constant text without NUL
	body, perr := syscall.UTF16PtrFromString(text)
	if perr != nil {
		body, _ = syscall.UTF16PtrFromString(appName + " " + what + ".") // ignore: constant text
	}
	const mbIconError = 0x10
	syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0,
		uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), mbIconError)
}
