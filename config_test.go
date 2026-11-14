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

func TestDefaultsAreValid(t *testing.T) {
	for _, s := range settings {
		var c config
		if err := s.apply(&c, s.def); err != nil {
			t.Errorf("default of %s is invalid: %v", s.key, err)
		}
		if strings.Contains(s.comment, "\n") {
			t.Errorf("comment of %s has a line break", s.key)
		}
		if got := s.get(defaultConfig()); got != s.def {
			t.Errorf("%s: get gives %q, default is %q", s.key, got, s.def)
		}
	}
	// The defaults of a download and of the settings file agree.
	c := defaultConfig()
	o := defaultOptions()
	if c.Defaults != o {
		t.Errorf("settings %+v\noptions  %+v", c.Defaults, o)
	}
}

func TestMissingFileIsCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "x.ini")
	res, err := loadConfig(path)
	if err != nil || !res.Created {
		t.Fatalf("created %v, err %v", res.Created, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range settings {
		if !strings.Contains(string(data), s.key+"="+s.def) {
			t.Errorf("new file lacks %s", s.key)
		}
	}
	again, err := loadConfig(path)
	if err != nil || again.Created || len(again.Added) > 0 || len(again.Warnings) > 0 {
		t.Fatalf("second read: %+v, %v", again, err)
	}
}

func TestInvalidValueWarnsAndUsesDefault(t *testing.T) {
	path := writeConfig(t, "quality=8k\nparallelDownloads=99\nfileName=../x.%(ext)s\n")
	res, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if res.Config.Defaults.Quality != "best" || res.Config.Parallel != 2 || res.Config.Defaults.Template != defaultOptions().Template {
		t.Fatalf("defaults not used: %+v", res.Config)
	}
	if len(res.Warnings) != 3 || !strings.Contains(res.Warnings[0], "line 1") {
		t.Fatalf("warnings: %q", res.Warnings)
	}
	if data, _ := os.ReadFile(path); !strings.HasPrefix(string(data), "quality=8k\n") { // ignore: compared right here
		t.Fatalf("user lines changed: %q", data)
	}
}

func TestUnknownKeyWarns(t *testing.T) {
	path := writeConfig(t, "quality=720\ncolour=blue\n")
	res, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if res.Config.Defaults.Quality != "720" {
		t.Fatalf("values not read: %+v", res.Config)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "colour") {
		t.Fatalf("warnings: %q", res.Warnings)
	}
}

func TestMissingKeysAreAppendedWithBackup(t *testing.T) {
	old := "\ufeff# mine\r\nquality=720\r\n"
	path := writeConfig(t, old)
	res, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added) != len(settings)-1 {
		t.Fatalf("added: %q", res.Added)
	}
	if res.Config.Defaults.Quality != "720" {
		t.Fatal("BOM broke the first key")
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil || string(backup) != old {
		t.Fatalf("backup %q, %v", backup, err)
	}
	data, _ := os.ReadFile(path) // ignore: checked below
	text := string(data)
	if !strings.HasPrefix(text, old) || strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") {
		t.Fatalf("file after append: %q", text)
	}
}

// Saving from the window changes values only: the user's comments, unknown
// lines, line endings and BOM stay.
func TestSaveKeepsTheUsersFile(t *testing.T) {
	old := "\ufeff# my notes\r\nquality=720\r\n# keep me\r\nfoo=bar\r\n"
	path := writeConfig(t, old)
	c := defaultConfig()
	c.Defaults.Quality, c.Parallel, c.DownloadDir = "1080", 3, filepath.Join(t.TempDir(), "v")
	if err := saveConfig(path, c); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path) // ignore: checked below
	text := string(data)
	if !strings.HasPrefix(text, "\ufeff# my notes\r\nquality=1080\r\n# keep me\r\nfoo=bar\r\n") {
		t.Fatalf("user lines changed: %q", text)
	}
	if strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") {
		t.Fatalf("mixed line endings: %q", text)
	}
	res, err := loadConfig(path)
	if err != nil || res.Config.Defaults.Quality != "1080" || res.Config.Parallel != 3 || res.Config.DownloadDir != c.DownloadDir {
		t.Fatalf("read back: %+v %v", res.Config, err)
	}
	if len(res.Warnings) != 1 || len(res.Added) != 0 { // only foo=bar
		t.Fatalf("after save: %+v", res)
	}
	if backup, _ := os.ReadFile(path + ".bak"); string(backup) != old { // ignore: compared right here
		t.Fatalf("backup: %q", backup)
	}

	c.Defaults.Template = "../evil.%(ext)s"
	if err := saveConfig(path, c); err == nil {
		t.Fatal("invalid value saved")
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x.ini")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWindowSize(t *testing.T) {
	for v, want := range map[string]windowState{
		"":              {defaultWindowWidth, defaultWindowHeight, false},
		"1600x900":      {1600, 900, false},
		"1600x900,max":  {1600, 900, true},
		"940x620":       {940, 620, false},
		"16384x16384":   {16384, 16384, false},
		"1240x820,max ": {defaultWindowWidth, defaultWindowHeight, false}, // not the form
	} {
		got, err := parseWindow(v)
		if got != want {
			t.Errorf("parseWindow(%q) = %+v, want %+v", v, got, want)
		}
		if (err != nil) != (strings.TrimSpace(v) != v) {
			t.Errorf("parseWindow(%q): %v", v, err)
		}
		if err == nil && v != "" && got.String() != v {
			t.Errorf("%q is written back as %q", v, got.String())
		}
	}
	// Too small, too large, not a size: refused, the default is used.
	for _, v := range []string{"160x28", "939x620", "940x619", "20000x900", "big", "1240", "1240x820x3", "-1240x820", "1240x820,min"} {
		if got, err := parseWindow(v); err == nil || got.Width != defaultWindowWidth || got.Height != defaultWindowHeight || got.Maximised {
			t.Errorf("parseWindow(%q) = %+v, %v", v, got, err)
		}
	}
}

// main.go reads the size before the window exists: it must not create or
// change the file, and a broken value gives the default.
func TestSavedWindowOnlyReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "VideoCatcher.ini")
	if got := savedWindow(path); got.Width != defaultWindowWidth || got.Maximised {
		t.Fatalf("no file: %+v", got)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the file was created")
	}
	for text, want := range map[string]windowState{
		"\ufeffquality=best\r\nWindow = 1500x1000,max\r\n": {1500, 1000, true},
		"# window=1x1\nwindow=1300x700\n":                  {1300, 700, false},
		"window=1x1\n":                                     {defaultWindowWidth, defaultWindowHeight, false},
		"quality=best\n":                                   {defaultWindowWidth, defaultWindowHeight, false},
	} {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := savedWindow(path); got != want {
			t.Errorf("%q: %+v, want %+v", text, got, want)
		}
		if after, err := os.ReadFile(path); err != nil || string(after) != text {
			t.Errorf("%q was changed", text)
		}
	}
}
