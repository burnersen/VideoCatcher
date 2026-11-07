// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func collect(in *intake, args ...string) []intakeRequest {
	var got []intakeRequest
	in.handleArgs(args, func(r intakeRequest) { got = append(got, r) })
	return got
}

func TestArgumentsBecomeRequests(t *testing.T) {
	in := newIntake(t.TempDir(), slog.New(slog.DiscardHandler))
	got := collect(in, "--add", "https://a.org/1", "--unknown",
		"videocatcher://add?url=https%3A%2F%2Fb.org%2F2&title=Hi&referer=https%3A%2F%2Fb.org%2F",
		"videocatcher://delete?url=x", "--add")
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if got[0].URL != "https://a.org/1" || got[0].Source != "cli" {
		t.Errorf("%+v", got[0])
	}
	if got[1].URL != "https://b.org/2" || got[1].Title != "Hi" || got[1].Referer != "https://b.org/" || got[1].Source != "protocol" {
		t.Errorf("%+v", got[1])
	}
}

func writeRequest(t *testing.T, in *intake, name string, req map[string]any) {
	t.Helper()
	if err := os.MkdirAll(in.inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(in.inbox, name), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRequestFileWithCookies(t *testing.T) {
	in := newIntake(t.TempDir(), slog.New(slog.DiscardHandler))
	writeRequest(t, in, "abcdef12-3456.json", map[string]any{"v": 1, "url": "https://paid.example/v/1",
		"source": "extension", "title": "T", "cookies": "# Netscape HTTP Cookie File\n.paid.example\tTRUE\t/\tTRUE\t0\tsid\tSECRET\n"})
	got := collect(in, "--request", "abcdef12-3456.json")
	if len(got) != 1 || got[0].Source != "extension" || got[0].CookieFile == "" || got[0].Cookies != "" {
		t.Fatalf("%+v", got)
	}
	if b, err := os.ReadFile(got[0].CookieFile); err != nil || !strings.Contains(string(b), "SECRET") {
		t.Fatalf("cookie file: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(in.inbox, "abcdef12-3456.json")); err == nil {
		t.Fatal("the request file stays after use")
	}
}

func TestBadRequestsAreRefused(t *testing.T) {
	dir := t.TempDir()
	in := newIntake(dir, slog.New(slog.DiscardHandler))
	secret := filepath.Join(dir, "secret.json")
	if err := os.WriteFile(secret, []byte(`{"v":1,"url":"https://x.org","source":"extension"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../secret.json", `..\secret.json`, secret, "short.json", "abcdefgh.txt"} {
		if got := collect(in, "--request", name); len(got) != 0 {
			t.Errorf("%q accepted: %+v", name, got)
		}
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatal("a file outside the inbox was touched")
	}
	writeRequest(t, in, "abcdefgh-v2.json", map[string]any{"v": 2, "url": "https://x.org", "source": "extension"})
	if got := collect(in, "--request", "abcdefgh-v2.json"); len(got) != 0 {
		t.Errorf("unknown version accepted: %+v", got)
	}
	// A request that does not say it is from the extension waits for the user.
	writeRequest(t, in, "abcdefgh-xx.json", map[string]any{"v": 1, "url": "https://x.org", "source": "cli"})
	if got := collect(in, "--request", "abcdefgh-xx.json"); len(got) != 1 || got[0].Source != "protocol" {
		t.Errorf("%+v", got)
	}
}

func TestClip(t *testing.T) {
	if got := clip("äöü", 3); got != "ä..." {
		t.Errorf("%q", got)
	}
	if got := clip("  abc  ", 10); got != "abc" {
		t.Errorf("%q", got)
	}
}
