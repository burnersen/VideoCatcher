// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrepareInstallsEverythingAndReports(t *testing.T) {
	key := newTestKey(t)
	rel := newFakeRelease(t, key, "2026.11.01", []byte("yt-dlp"))
	rel.serve(t)
	trustKey(key)

	archive := makeZip(t, map[string]string{"ff/bin/ffmpeg.exe": "a", "ff/bin/ffprobe.exe": "b"})
	sum := sha256.Sum256(archive)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) }))
	defer srv.Close()
	rt := runtimeFile{Format: 1, Tools: []runtimeEntry{{Tool: "ffmpeg", Platform: "test/x", Name: "FFmpeg 1",
		File: "ff.zip", Size: int64(len(archive)), SHA256: hex.EncodeToString(sum[:]),
		Extract: []string{"bin/ffmpeg.exe", "bin/ffprobe.exe"}, URLs: []string{srv.URL + "/ff.zip"}}}}

	tb := newToolbox(newToolSet(t.TempDir()), newHTTPClient(), slog.New(slog.DiscardHandler), rt)
	tb.plat = "test/x"
	reports := 0
	tb.report = func([]ToolStatus) { reports++ }
	if err := tb.Prepare(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	got := map[string]ToolStatus{}
	for _, s := range tb.Statuses() {
		got[s.Tool] = s
	}
	if got["yt-dlp"].State != toolReady || got["yt-dlp"].Version != "2026.11.01" {
		t.Errorf("yt-dlp: %+v", got["yt-dlp"])
	}
	if got["ffmpeg"].State != toolReady || got["ffmpeg"].Version != "FFmpeg 1" {
		t.Errorf("ffmpeg: %+v", got["ffmpeg"])
	}
	if got["deno"].State != toolMissing || got["deno"].Error == "" {
		t.Errorf("deno has no build for test/x and must say so: %+v", got["deno"])
	}
	if reports == 0 {
		t.Error("the window was never told")
	}

	// A second start without update check reads tools.json and downloads nothing.
	served := rel.served
	tb2 := newToolbox(tb.ts, newHTTPClient(), slog.New(slog.DiscardHandler), rt)
	tb2.plat = "test/x"
	if err := tb2.Prepare(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if rel.served != served || !tb2.Ready("yt-dlp") || !tb2.Ready("ffmpeg") {
		t.Fatalf("second start: downloads %d, %+v", rel.served-served, tb2.Statuses())
	}
}

// A failed update check keeps a working yt-dlp usable and says why.
func TestFailedCheckKeepsTheInstalledYtDlp(t *testing.T) {
	key := newTestKey(t)
	rel := newFakeRelease(t, key, "2026.11.01", []byte("yt-dlp"))
	rel.serve(t)
	trustKey(key)
	tb := newToolbox(newToolSet(t.TempDir()), newHTTPClient(), slog.New(slog.DiscardHandler), runtimeFile{Format: 1})
	if err := tb.Prepare(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	rel.tag = "2026.12.01"
	rel.sig = rel.sig[:len(rel.sig)-1] // the new release is broken
	if err := tb.Prepare(context.Background(), true); err == nil {
		t.Fatal("broken release accepted")
	}
	s := tb.Statuses()[0]
	if s.State != toolReady || s.Version != "2026.11.01" || !strings.Contains(s.Error, "signature") {
		t.Fatalf("%+v", s)
	}
}
