// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// The page gets message and hint on two lines, never the technical cause.
func TestUIErrorShowsMessageAndHint(t *testing.T) {
	app := NewApp(appPaths{LogFile: "x.log", DataDir: t.TempDir()}, slog.New(slog.DiscardHandler))
	err := app.uiError(fileError(errors.New("no such file or directory"), "open", `C:\x.txt`))
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `C:\x.txt`) || !strings.HasPrefix(lines[1], "Try") {
		t.Fatalf("text for the page: %q", err.Error())
	}
	if strings.Contains(err.Error(), "no such file") {
		t.Fatalf("technical cause reached the page: %q", err.Error())
	}
}

func TestUIErrorUnknownPointsToLog(t *testing.T) {
	app := NewApp(appPaths{LogFile: `C:\logs\videocatcher.log`, DataDir: t.TempDir()}, slog.New(slog.DiscardHandler))
	err := app.uiError(errors.New("boom"))
	if !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), `C:\logs\videocatcher.log`) {
		t.Fatalf("%q", err.Error())
	}
}

// Links that reach the app from outside take the defaults of the settings
// and the folder; a link from a web page waits for the user.
func TestAcceptUsesTheSettings(t *testing.T) {
	app := NewApp(appPaths{LogFile: "x.log", DataDir: t.TempDir()}, slog.New(slog.DiscardHandler))
	app.cfg.DownloadDir = t.TempDir()
	app.cfg.Defaults.Quality = "720"
	app.accept(intakeRequest{URL: "https://example.org/a", Source: "protocol"})
	jobs := app.queue.List()
	if len(jobs) != 1 || jobs[0].Status != jobConfirm || jobs[0].Options.Quality != "720" || jobs[0].Options.OutputDir != app.cfg.DownloadDir {
		t.Fatalf("%+v", jobs)
	}
}
