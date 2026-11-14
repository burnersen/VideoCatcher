// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
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

// The same link sent twice from the browser while it still waits: one job.
// Once it is done, sending it again downloads it again.
func TestAcceptSkipsALinkThatStillWaits(t *testing.T) {
	app := NewApp(appPaths{LogFile: "x.log", DataDir: t.TempDir()}, slog.New(slog.DiscardHandler))
	app.cfg.DownloadDir = t.TempDir()
	app.queue.Hold()
	cookies := filepath.Join(t.TempDir(), "c.txt")
	if err := os.WriteFile(cookies, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	app.accept(intakeRequest{URL: "https://example.org/a", Source: "extension"})
	app.accept(intakeRequest{URL: "https://example.org/a", Source: "extension", CookieFile: cookies})
	jobs := app.queue.List()
	if len(jobs) != 1 {
		t.Fatalf("%d jobs", len(jobs))
	}
	if _, err := os.Stat(cookies); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the cookie file of the link that was not added is still there: %v", err)
	}
	if app.Known("https://example.org/a") != jobWaiting || app.Known("https://example.org/b") != "" {
		t.Fatalf("Known: %q %q", app.Known("https://example.org/a"), app.Known("https://example.org/b"))
	}
	if err := app.queue.Cancel(jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	app.accept(intakeRequest{URL: "https://example.org/a", Source: "extension"})
	if n := len(app.queue.List()); n != 2 {
		t.Fatalf("%d jobs after the first was stopped", n)
	}
}

func TestWindowTitle(t *testing.T) {
	for _, c := range []struct {
		running int
		percent float64
		want    string
	}{
		{0, 0, "Video-Catcher"},
		{1, 42.9, "42 % · Video-Catcher"},
		{3, 7, "7 % · 3 downloads · Video-Catcher"},
	} {
		if got := windowTitle(c.running, c.percent); got != c.want {
			t.Errorf("windowTitle(%d, %v) = %q", c.running, c.percent, got)
		}
	}
}

// "Copy details" has what a bug report needs, and no login.
func TestJobReport(t *testing.T) {
	got := jobReport(Job{URL: "https://example.org/v", Title: "A video", Status: jobFailed, Stage: "failed",
		Error: "The site refused the download\nClick \"Check for updates\".", Details: []string{"Error: HTTP Error 403: Forbidden"},
		CookieName: "secret-cookies.txt"})
	for _, want := range []string{"Video-Catcher " + version, "Title: A video", "Link: https://example.org/v", "State: failed",
		"Message: The site refused the download Click", "  Error: HTTP Error 403: Forbidden"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "secret-cookies") {
		t.Errorf("the cookie file is named:\n%s", got)
	}
}

// Only files a job reported are opened, and only video and audio files are
// handed to Windows: a site decides what a download is called.
func TestOpenFileOnlyPlaysMedia(t *testing.T) {
	for _, name := range []string{"a.mp4", "a.MKV", "a.mp3", "a.opus"} {
		if !playable[strings.ToLower(filepath.Ext(name))] {
			t.Errorf("%s is not played", name)
		}
	}
	for _, name := range []string{"a.exe", "a.bat", "a.lnk", "a.html", "a.url", "a", "a.mp4.exe", "a.srt"} {
		if playable[strings.ToLower(filepath.Ext(name))] {
			t.Errorf("%s would be opened", name)
		}
	}
	app := NewApp(appPaths{LogFile: "x.log", DataDir: t.TempDir()}, slog.New(slog.DiscardHandler))
	if _, err := app.jobFile("nope", 0); err == nil {
		t.Fatal("a file of an unknown job")
	}
}
