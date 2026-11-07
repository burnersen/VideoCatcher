// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The rule of NVENCForge: letters and digits, one dot between, none at the
// ends; "#2" becomes "Nr2"; letters of every script stay.
func TestTidyName(t *testing.T) {
	for in, want := range map[string]string{
		"[ABC-123] A long title… with (brackets) ⋆ Site ⋆ More": "ABC.123.A.long.title.with.brackets.Site.More",
		"Holiday 2026 (Part #2) - Sea":                          "Holiday.2026.Part.Nr2.Sea",
		"Ärger über Öl & Straße":                                "Ärger.über.Öl.Straße",
		"東京 タワー 2026":                                           "東京.タワー.2026",
		"Привет, мир!":                                          "Привет.мир",
		"नमस्ते दुनिया":                                         "नमस्ते.दुनिया",
		"already.tidy.name":                                     "already.tidy.name",
		"...dots...everywhere...":                               "dots.everywhere",
		"#hashtag #1":                                           "hashtag.Nr1",
		"emoji 🎬 film":                                          "emoji.film",
		"!!!":                                                   "",
		"CON":                                                   "",
	} {
		if got := tidyName(in); got != want {
			t.Errorf("tidyName(%q) = %q, want %q", in, got, want)
		}
	}
	long := tidyName(strings.Repeat("ä", 150))
	if len(long) > maxFileNameLen || strings.ContainsRune(long, '\uFFFD') {
		t.Errorf("long name: %d bytes", len(long))
	}
}

func TestTidyPathKeepsFolderAndExtension(t *testing.T) {
	if got := tidyPath(`D:\Videos\My film [abc].mp4`); got != `D:\Videos\My.film.abc.mp4` {
		t.Error(got)
	}
	if got := tidyPath(`D:\Videos\!!!.mp4`); got != `D:\Videos\!!!.mp4` {
		t.Error("a name with nothing left must stay:", got)
	}
}

// A tidy name that is taken gets a number; the other file is never touched.
func TestTidyFileDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	taken := filepath.Join(dir, "My.film.mp4")
	if err := os.WriteFile(taken, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "My film.mp4")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := tidyFile(src)
	if err != nil || got != filepath.Join(dir, "My.film.2.mp4") {
		t.Fatalf("%q %v", got, err)
	}
	if b, _ := os.ReadFile(taken); string(b) != "old" {
		t.Fatal("existing file changed")
	}
}

// Only files that belong to the video move with it; another video with a
// longer name that starts the same stays.
func TestTidyFileTakesOnlyCompanions(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Film [a].mkv", "Film [a].en.vtt", "Film [a].webp", "Film [a].part 2.mkv"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tidyFile(filepath.Join(dir, "Film [a].mkv")); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"Film.a.mkv", "Film.a.en.vtt", "Film.a.webp", "Film [a].part 2.mkv"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Error(err)
		}
	}
}
