// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Live test with the real programs (06 T5): downloads yt-dlp (with the real
// signature check), FFmpeg and Deno, asks yt-dlp about a real video and
// downloads it. Runs only with VIDEOCATCHER_TEST_LIVE=1, in the workflow
// "Live test" on GitHub; never in the normal CI and never in a cloud
// session that may not run downloaded programs.

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

// liveTestURL is a short video that is meant for tests: yt-dlp's own test
// video. VIDEOCATCHER_TEST_URL replaces it, for sites that block the
// runners of GitHub.
const liveTestURL = "https://www.youtube.com/watch?v=BaW_jenozKc"

func TestLiveRealDownload(t *testing.T) {
	if os.Getenv("VIDEOCATCHER_TEST_LIVE") != "1" {
		t.Skip("VIDEOCATCHER_TEST_LIVE not set")
	}
	link := liveTestURL
	if u := os.Getenv("VIDEOCATCHER_TEST_URL"); u != "" {
		link = u
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	data := t.TempDir()
	rt, err := loadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	tb := newToolbox(newToolSet(data), newHTTPClient(), logger, rt)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := tb.Prepare(ctx, true); err != nil {
		t.Fatalf("prepare: %v\n%+v", err, tb.Statuses())
	}
	t.Logf("tools: %+v", tb.Statuses())

	info, err := probe(ctx, execRunner{}, tb.ts.YtDlp, probeArgs(link, probeSettings{Playlist: "single"}, tb.ts, tb.Ready, ""))
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	t.Logf("probe: %q, %d formats, heights %v", info.Title, len(info.Formats), info.Heights)

	q := newQueue(execRunner{}, tb.ts, tb.Ready, data, logger)
	defer q.Close()
	o := defaultOptions()
	o.OutputDir, o.Quality, o.Container = t.TempDir(), "360", "mp4"
	j, err := q.Add(link, o, "window", "", info.Title, "")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Minute)
	for {
		got := jobByID(q, j.ID)
		if isFinished(got.Status) {
			if got.Status != jobDone || len(got.Files) == 0 {
				t.Fatalf("download: %+v", got)
			}
			st, err := os.Stat(got.Files[0])
			if err != nil || st.Size() == 0 {
				t.Fatalf("file %s: %v", got.Files[0], err)
			}
			t.Logf("downloaded %s, %d bytes", got.Files[0], st.Size())
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("still not done: %+v", got)
		}
		time.Sleep(time.Second)
	}
}
