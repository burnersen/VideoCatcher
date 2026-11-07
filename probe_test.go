// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// fakeRunner plays the part of yt-dlp: it prints the given lines and ends
// with err. With wait set, it runs until the context ends.
type fakeRunner struct {
	mu    sync.Mutex
	calls [][]string
	lines func(args []string) []string
	err   error
	wait  bool
}

func (f *fakeRunner) Run(ctx context.Context, exe string, args []string, onLine func(bool, string)) error {
	f.mu.Lock()
	f.calls = append(f.calls, args)
	f.mu.Unlock()
	if f.lines != nil {
		for _, l := range f.lines(args) {
			onLine(strings.HasPrefix(l, "ERROR") || strings.HasPrefix(l, "WARNING"), l)
		}
	}
	if f.wait {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.err
}

const sampleVideo = `{"_type":"video","id":"abc","title":"A test","uploader":"Someone","extractor_key":"Youtube","duration":212,
"webpage_url":"https://www.youtube.com/watch?v=abc","thumbnail":"https://i.ytimg.com/vi/abc/hq.jpg",
"formats":[
 {"format_id":"sb0","ext":"mhtml","vcodec":"none","acodec":"none"},
 {"format_id":"140","ext":"m4a","vcodec":"none","acodec":"mp4a.40.2","tbr":129.5,"filesize":3400000},
 {"format_id":"137","ext":"mp4","height":1080,"fps":30,"vcodec":"avc1.640028","acodec":"none","tbr":4400,"filesize_approx":120000000},
 {"format_id":"22","ext":"mp4","height":720,"fps":30,"vcodec":"avc1","acodec":"mp4a.40.2","tbr":1200},
 {"format_id":"399","ext":"mp4","height":1080,"vcodec":"av01.0.08M.08","acodec":"none","tbr":2200}],
"subtitles":{"de":[],"en":[],"live_chat":[]},"automatic_captions":{"en-orig":[],"xx":[],"fr":[]}}`

func TestProbeReadsAVideo(t *testing.T) {
	r := &fakeRunner{lines: func([]string) []string { return []string{"WARNING: slow", sampleVideo} }}
	m, err := probe(context.Background(), r, "yt-dlp", []string{"-J"})
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "A test" || m.Uploader != "Someone" || m.Site != "Youtube" || m.Duration != 212 || m.IsPlaylist {
		t.Errorf("%+v", m)
	}
	if len(m.Formats) != 4 || m.Formats[0].ID != "137" || m.Formats[len(m.Formats)-1].ID != "140" {
		t.Errorf("formats: %+v", m.Formats)
	}
	if !m.Formats[0].Approx || m.Formats[0].VCodec != "avc1" || m.Formats[0].HasAudio {
		t.Errorf("first format: %+v", m.Formats[0])
	}
	if fmt.Sprint(m.Heights) != "[1080 720]" {
		t.Errorf("heights %v", m.Heights)
	}
	if strings.Join(m.Subtitles, ",") != "de,en" || strings.Join(m.AutoSubs, ",") != "en-orig,fr" {
		t.Errorf("subtitles %v / %v", m.Subtitles, m.AutoSubs)
	}
	if len(m.Warnings) != 1 || len(m.Raw) == 0 {
		t.Errorf("warnings %v", m.Warnings)
	}
}

func TestProbeReadsAPlaylist(t *testing.T) {
	r := &fakeRunner{lines: func([]string) []string {
		return []string{`{"_type":"playlist","title":"List","playlist_count":3,"entries":[{"title":"a","duration":1},{"title":"b"},{"title":"c"}]}`}
	}}
	m, err := probe(context.Background(), r, "yt-dlp", nil)
	if err != nil || !m.IsPlaylist || m.Count != 3 || len(m.Entries) != 3 || m.Entries[2].Index != 3 {
		t.Fatalf("%+v %v", m, err)
	}
}

func TestProbeExplainsErrors(t *testing.T) {
	r := &fakeRunner{lines: func([]string) []string { return []string{"ERROR: [generic] Unsupported URL: https://x.org"} }, err: errors.New("exit 1")}
	_, err := probe(context.Background(), r, "yt-dlp", nil)
	if err == nil || !strings.Contains(err.Error(), "does not know this site") {
		t.Fatalf("%v", err)
	}
}

func TestProbeArgs(t *testing.T) {
	ts := newToolSet(t.TempDir())
	a := probeArgs("https://x.org", probeSettings{Playlist: "all", Cookies: "firefox"}, ts, allTools, "")
	if !pair(a, "--cookies-from-browser", "firefox") || a[len(a)-1] != "https://x.org" || a[len(a)-2] != "--" {
		t.Errorf("%v", a)
	}
}
