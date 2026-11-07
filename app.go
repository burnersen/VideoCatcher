// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// version is set by the release build with -ldflags "-X main.version=1.2.3".
// Local builds say "dev". Never edit it by hand: the tag is the only source.
var version = "dev"

const (
	appName = "VideoCatcher" // shown to users, file names on Windows
	appID   = "videocatcher" // lower case: log file name
)

// App is everything the page may call. Wails turns every exported method
// into a JavaScript function on window.go.main.App. Rules:
//
//   - one job per method, no logic in the page that the Go side should own
//   - every error goes through uiError: the page gets text a user can act
//     on, the log gets the technical cause
//   - nothing here blocks for long; long work runs in a goroutine and
//     reports with events (wailsruntime.EventsEmit)
type App struct {
	ctx    context.Context
	paths  appPaths
	logger *slog.Logger
}

func NewApp(paths appPaths, logger *slog.Logger) *App {
	return &App{paths: paths, logger: logger}
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// onSecondInstance brings this window to the front when the program is
// started a second time.
func (a *App) onSecondInstance(options.SecondInstanceData) {
	wailsruntime.WindowUnminimise(a.ctx)
	wailsruntime.WindowShow(a.ctx)
}

// Version is shown in the window's footer.
func (a *App) Version() string { return version }

// FileResult is what the page shows for one file.
type FileResult struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// PickFile opens the Windows file dialog. An empty path means: cancelled.
func (a *App) PickFile() (string, error) {
	path, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "Choose a file"})
	if err != nil {
		return "", a.uiError(userError(err, "The file dialog did not open", "Please try again."))
	}
	return path, nil
}

// CheckFile returns size and SHA-256 of a file.
func (a *App) CheckFile(path string) (FileResult, error) {
	res, err := checkFile(path)
	if err != nil {
		return FileResult{}, a.uiError(err)
	}
	a.logger.Info("checked", "file", path, "size", res.Size)
	return res, nil
}

// checkFile is the logic behind CheckFile, without Wails: tests call it
// directly, on any system.
func checkFile(path string) (FileResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileResult{}, fileError(err, "open", path)
	}
	defer f.Close() // read only: a close error changes nothing
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return FileResult{}, fileError(err, "read", path)
	}
	return FileResult{Path: path, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}
