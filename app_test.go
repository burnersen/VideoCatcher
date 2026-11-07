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

func TestCheckFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := checkFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if res.Size != 3 || res.SHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("%+v", res)
	}
}

// The page gets message and hint on two lines, never the technical cause.
func TestUIErrorShowsMessageAndHint(t *testing.T) {
	app := NewApp(appPaths{LogFile: "x.log"}, slog.New(slog.DiscardHandler))
	_, err := app.CheckFile(filepath.Join(t.TempDir(), "missing.txt"))
	if err == nil {
		t.Fatal("no error for a missing file")
	}
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "does not exist") || !strings.HasPrefix(lines[1], "Check") {
		t.Fatalf("text for the page: %q", err.Error())
	}
	if strings.Contains(err.Error(), "no such file") {
		t.Fatalf("technical cause reached the page: %q", err.Error())
	}
}

func TestUIErrorUnknownPointsToLog(t *testing.T) {
	app := NewApp(appPaths{LogFile: `C:\logs\videocatcher.log`}, slog.New(slog.DiscardHandler))
	err := app.uiError(errors.New("boom"))
	if !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), `C:\logs\videocatcher.log`) {
		t.Fatalf("%q", err.Error())
	}
}
