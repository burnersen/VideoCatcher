// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// What the browser sent goes on: the login in a header, the player's own
// headers, the Origin. What yt-dlp sets itself, what has its own way and
// what is no header at all stays out.
func TestSplitHeaders(t *testing.T) {
	ua, rest := splitHeaders(map[string]string{
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:140.0) Gecko/20100101 Firefox/140.0",
		"Authorization":   "Bearer abc.def-ghi",
		"Origin":          "https://player.example",
		"X-Token":         `a"b\c`,
		"Cookie":          "sid=1",
		"Referer":         "https://x.example/",
		"Range":           "bytes=0-",
		"Host":            "evil.example",
		"Sec-Fetch-Mode":  "cors",
		"If-None-Match":   "1",
		"Accept-Encoding": "gzip",
		"Bad Name":        "x",
		"X-Newline":       "a\nb",
		"X-Umlaut":        "ä",
		"X-Empty":         "",
		"X-Space":         " a",
	})
	if !strings.HasPrefix(ua, "Mozilla/5.0") {
		t.Errorf("user agent %q", ua)
	}
	var names []string
	for _, h := range rest {
		names = append(names, h.Name)
	}
	if !slices.Equal(names, []string{"Authorization", "Origin", "X-Token"}) {
		t.Errorf("headers %v", names)
	}
	if ua, rest := splitHeaders(map[string]string{"user-agent": "a\r\nX: y"}); ua != "" || len(rest) != 0 {
		t.Errorf("a broken user agent is used: %q %v", ua, rest)
	}
	many := map[string]string{}
	for i := 0; i < 40; i++ {
		many["X-H"+string(rune('a'+i%26))+string(rune('a'+i/26))] = "v"
	}
	if _, rest := splitHeaders(many); len(rest) != maxHeaders {
		t.Errorf("%d headers, at most %d", len(rest), maxHeaders)
	}
}

// The file yt-dlp reads: one option per line, and no value can end its
// quotes and become an option of its own.
func TestHeaderConfig(t *testing.T) {
	got := headerConfig([]header{{"Authorization", "Bearer abc"}, {"X-Token", `a"b\c --exec "calc"`}})
	want := "# coding: utf-8\n# Written by Video-Catcher for one download.\n" +
		"--add-headers \"Authorization:Bearer abc\"\n" +
		"--add-headers \"X-Token:a\\\"b\\\\c --exec \\\"calc\\\"\"\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if strings.Count(got, "\n") != 4 {
		t.Errorf("lines: %q", got)
	}
}

// A request of the extension with headers: the secrets go into a file of
// their own, the browser's name stays with the request, nothing stays in
// the request itself.
func TestRequestFileWithHeaders(t *testing.T) {
	in := newIntake(t.TempDir(), slog.New(slog.DiscardHandler))
	writeRequest(t, in, "abcdef12-9999.json", map[string]any{"v": 1, "url": "https://cdn.example/a/master.m3u8?t=1",
		"pageUrl": "https://paid.example/watch/1", "source": "extension",
		"headers": map[string]string{"Authorization": "Bearer SECRET", "User-Agent": "Mozilla/5.0 Test", "Cookie": "no"}})
	got := collect(in, "--request", "abcdef12-9999.json")
	if len(got) != 1 || got[0].HeaderFile == "" || got[0].Headers != nil || got[0].UserAgent != "Mozilla/5.0 Test" {
		t.Fatalf("%+v", got)
	}
	b, err := os.ReadFile(got[0].HeaderFile)
	if err != nil || !strings.Contains(string(b), `--add-headers "Authorization:Bearer SECRET"`) || strings.Contains(string(b), "Cookie") ||
		strings.Contains(string(b), "Mozilla") {
		t.Fatalf("header file: %q %v", b, err)
	}
	if filepath.Dir(got[0].HeaderFile) != in.cookies {
		t.Errorf("header file outside the cookie folder: %s", got[0].HeaderFile)
	}
	// Only a browser name: no file is needed.
	writeRequest(t, in, "abcdef12-8888.json", map[string]any{"v": 1, "url": "https://paid.example/watch/1",
		"source": "extension", "headers": map[string]string{"User-Agent": "Mozilla/5.0 Test"}})
	if got := collect(in, "--request", "abcdef12-8888.json"); len(got) != 1 || got[0].HeaderFile != "" || got[0].UserAgent == "" {
		t.Fatalf("%+v", got)
	}
}

// The secrets of the headers reach yt-dlp as a file, never as an argument;
// the browser's name is an argument.
func TestHeadersInTheArguments(t *testing.T) {
	a := buildArgs(testOptions(t), "https://x.org", newToolSet(t.TempDir()), allTools,
		jobPaths{Temp: "T", Headers: "H.conf"}, "https://page.org/a", "Mozilla/5.0 Test")
	if !pair(a, "--config-locations", "H.conf") || !pair(a, "--add-headers", "User-Agent:Mozilla/5.0 Test") ||
		!pair(a, "--add-headers", "Referer:https://page.org/a") || !slices.Contains(a, "--ignore-config") {
		t.Errorf("%v", a)
	}
	if a[len(a)-2] != "--" {
		t.Errorf("the link is not last: %v", a)
	}
}
