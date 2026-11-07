// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func testOptions(t *testing.T) DownloadOptions {
	o := defaultOptions()
	o.OutputDir = t.TempDir()
	return o
}

func noTools(string) bool  { return false }
func allTools(string) bool { return true }

func TestDefaultOptionsAreValid(t *testing.T) {
	if err := testOptions(t).validate(); err != nil {
		t.Fatal(err)
	}
}

// pair says whether args holds flag directly followed by value.
func pair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestBuildArgsBasics(t *testing.T) {
	o := testOptions(t)
	ts := newToolSet(t.TempDir())
	a := buildArgs(o, "https://example.org/v", ts, allTools, jobPaths{Temp: "T"}, "")
	if a[len(a)-2] != "--" || a[len(a)-1] != "https://example.org/v" {
		t.Fatalf("the link must come last, after --: %v", a[len(a)-3:])
	}
	for _, want := range [][2]string{{"-P", o.OutputDir}, {"-P", "temp:T"}, {"-o", o.Template}, {"-f", "bv*+ba/b"},
		{"--ffmpeg-location", ts.Dir}, {"--js-runtimes", "deno:" + ts.Deno}} {
		if !pair(a, want[0], want[1]) {
			t.Errorf("missing %s %s in %v", want[0], want[1], a)
		}
	}
	if !slices.Contains(a, "--ignore-config") || !slices.Contains(a, "--no-playlist") {
		t.Errorf("%v", a)
	}
	b := buildArgs(o, "https://example.org/v", ts, noTools, jobPaths{Temp: "T"}, "")
	if slices.Contains(b, "--ffmpeg-location") || slices.Contains(b, "--js-runtimes") {
		t.Errorf("tools that are not there are passed: %v", b)
	}
}

// README promise: an existing file in the download folder is never
// overwritten.
func TestNeverOverwritesExistingFiles(t *testing.T) {
	o := testOptions(t)
	for _, q := range qualityChoices {
		o.Quality = q
		a := buildArgs(o, "https://x.org", newToolSet(t.TempDir()), allTools, jobPaths{Temp: "T"}, "")
		if !slices.Contains(a, "--no-overwrites") || slices.Contains(a, "--force-overwrites") {
			t.Fatalf("quality %s: %v", q, a)
		}
	}
}

func TestQualityAndAudio(t *testing.T) {
	o := testOptions(t)
	ts := newToolSet(t.TempDir())
	o.Quality, o.Container = "1080", "mp4"
	a := buildArgs(o, "https://x.org", ts, allTools, jobPaths{Temp: "T"}, "")
	if !pair(a, "-S", "res:1080,ext:mp4:m4a") || !pair(a, "--merge-output-format", "mp4") {
		t.Errorf("1080 mp4: %v", a)
	}
	o.Quality, o.AudioFormat, o.AudioQuality = "audio", "mp3", "192"
	a = buildArgs(o, "https://x.org", ts, allTools, jobPaths{Temp: "T"}, "")
	if !pair(a, "-f", "ba/b") || !slices.Contains(a, "-x") || !pair(a, "--audio-format", "mp3") || !pair(a, "--audio-quality", "192K") {
		t.Errorf("audio: %v", a)
	}
	if slices.Contains(a, "--merge-output-format") {
		t.Errorf("audio with a video container: %v", a)
	}
	o.FormatID = "137+140"
	a = buildArgs(o, "https://x.org", ts, allTools, jobPaths{Temp: "T"}, "")
	if !pair(a, "-f", "137+140") || slices.Contains(a, "-x") {
		t.Errorf("format id: %v", a)
	}
}

func TestSubtitleModes(t *testing.T) {
	o := testOptions(t)
	o.SubLangs = "de,en"
	cases := map[string][]string{
		"embed": {"--embed-subs"},
		"file":  {"--write-subs", "--convert-subs"},
		"both":  {"--embed-subs", "--write-subs"},
	}
	for mode, want := range cases {
		o.SubMode = mode
		a := buildArgs(o, "https://x.org", newToolSet(t.TempDir()), allTools, jobPaths{Temp: "T"}, "")
		for _, w := range want {
			if !slices.Contains(a, w) || !pair(a, "--sub-langs", "de,en") {
				t.Errorf("%s: %v", mode, a)
			}
		}
		if mode == "embed" && slices.Contains(a, "--write-subs") {
			t.Errorf("embed only keeps no separate file: %v", a)
		}
	}
}

func TestCookiesAndReferer(t *testing.T) {
	o := testOptions(t)
	o.Cookies = "firefox"
	a := buildArgs(o, "https://x.org", newToolSet(t.TempDir()), allTools, jobPaths{Temp: "T"}, "https://page.org/a")
	if !pair(a, "--cookies-from-browser", "firefox") || !pair(a, "--add-headers", "Referer:https://page.org/a") {
		t.Errorf("%v", a)
	}
	// A cookie file from the browser extension wins over the browser.
	a = buildArgs(o, "https://x.org", newToolSet(t.TempDir()), allTools, jobPaths{Temp: "T", Cookies: "C"}, "")
	if !pair(a, "--cookies", "C") || slices.Contains(a, "--cookies-from-browser") {
		t.Errorf("%v", a)
	}
}

func TestInvalidOptionsAreRefused(t *testing.T) {
	cases := map[string]func(o *DownloadOptions){
		"quality":          func(o *DownloadOptions) { o.Quality = "8k" },
		"format with ;":    func(o *DownloadOptions) { o.FormatID = "1; rm" },
		"option as format": func(o *DownloadOptions) { o.FormatID = "--exec calc" },
		"sub langs":        func(o *DownloadOptions) { o.SubMode, o.SubLangs = "embed", "de;en" },
		"items":            func(o *DownloadOptions) { o.PlaylistItems = "1-3;x" },
		"rate":             func(o *DownloadOptions) { o.RateLimit = "fast" },
		"fragments":        func(o *DownloadOptions) { o.Fragments = 99 },
		"relative folder":  func(o *DownloadOptions) { o.OutputDir = "downloads" },
		"cookies":          func(o *DownloadOptions) { o.Cookies = "netscape" },
	}
	for name, spoil := range cases {
		o := testOptions(t)
		spoil(&o)
		if err := o.validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	o := testOptions(t)
	o.SubMode, o.SubLangs = "embed", "de,en,-live_chat"
	if err := o.validate(); err != nil {
		t.Errorf("good languages refused: %v", err)
	}
}

// README promise: files go only into the chosen folder.
func TestTemplateStaysInTheFolder(t *testing.T) {
	for _, bad := range []string{"", "../%(title)s.%(ext)s", `..\x.%(ext)s`, "a/../../%(ext)s", "/abs/%(title)s.%(ext)s",
		`C:\x\%(title)s.%(ext)s`, `\\server\share\%(ext)s`, "%(title)s", strings.Repeat("a", 400) + ".%(ext)s"} {
		if err := checkTemplate(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, good := range []string{"%(title)s.%(ext)s", "%(uploader)s/%(title)s [%(id)s].%(ext)s", "%(playlist)s/%(playlist_index)03d %(title)s.%(ext)s", "a..b.%(ext)s"} {
		if err := checkTemplate(good); err != nil {
			t.Errorf("refused %q: %v", good, err)
		}
	}
}

func TestCheckURL(t *testing.T) {
	for _, bad := range []string{"", "   ", "file:///C:/x", "javascript:alert(1)", "--exec calc", "ftp://x.org/a", "https://", strings.Repeat("a", 5000)} {
		if _, err := checkURL(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if u, err := checkURL("  https://www.youtube.com/watch?v=abc&t=1 \n"); err != nil || u != "https://www.youtube.com/watch?v=abc&t=1" {
		t.Errorf("%q %v", u, err)
	}
}

func TestFinalExtension(t *testing.T) {
	o := testOptions(t)
	if got := finalExtension(filepath.Join("d", "a.b.webm"), o); got != filepath.Join("d", "a.b.webm") {
		t.Errorf("auto changed the name: %s", got)
	}
	o.Container = "mkv"
	if got := finalExtension("a.b.webm", o); got != "a.b.mkv" {
		t.Errorf("mkv: %s", got)
	}
	o.Quality, o.AudioFormat = "audio", "mp3"
	if got := finalExtension("song.webm", o); got != "song.mp3" {
		t.Errorf("mp3: %s", got)
	}
}
