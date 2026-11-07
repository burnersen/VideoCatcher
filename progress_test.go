// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"errors"
	"strings"
	"testing"
)

func TestParseProgressLines(t *testing.T) {
	l := parseLine("VCPROG downloading|1048576|10485760|NA|524288.5|18|NA|NA|137")
	if l.Kind != lineProgress || l.Done != 1048576 || l.Total != 10485760 || l.Estimated || l.Speed != 524288.5 || l.ETA != 18 || l.FormatID != "137" {
		t.Errorf("%+v", l)
	}
	// HLS: no total, only an estimate; fragments; no ETA.
	l = parseLine("VCPROG downloading|2000.0|NA|50000.5|NA|NA|3|40|hls-720p\r")
	if l.Total != 50000 || !l.Estimated || l.ETA != -1 || l.Speed != 0 || l.Fragment != 3 || l.Fragments != 40 {
		t.Errorf("%+v", l)
	}
	if l := parseLine("VCPROG broken"); !l.Unreadable {
		t.Errorf("%+v", l)
	}
	if l := parseLine("VCPOST started|Merger"); l.Kind != linePost || l.PostStep != "Merger" || postStepLabel(l.PostStep) != "joining picture and sound" {
		t.Errorf("%+v", l)
	}
	// Titles may contain the separator.
	if l := parseLine("VCITEM 2|10|abc|A | B"); l.Index != 2 || l.Count != 10 || l.Title != "A | B" {
		t.Errorf("%+v", l)
	}
	if l := parseLine("VCITEM NA|NA|abc|T"); l.Index != 0 || l.Count != 0 {
		t.Errorf("single video: %+v", l)
	}
	if l := parseLine(`VCFILE C:\Videos\a b.mp4`); l.Kind != lineFile || l.Path != `C:\Videos\a b.mp4` {
		t.Errorf("%+v", l)
	}
	if l := parseLine("ERROR: [youtube] x: Video unavailable"); l.Kind != lineError || l.Text != "[youtube] x: Video unavailable" {
		t.Errorf("%+v", l)
	}
	if l := parseLine("WARNING: something"); l.Kind != lineWarning {
		t.Errorf("%+v", l)
	}
	if l := parseLine("[download] Destination: x"); l.Kind != lineOther {
		t.Errorf("%+v", l)
	}
}

func TestYtDlpErrorsAreExplained(t *testing.T) {
	cases := map[string]string{
		"[youtube] abc: Sign in to confirm you're not a bot. Use --cookies-from-browser":          "logged-in users",
		"[generic] Unsupported URL: https://example.org/":                                         "does not know this site",
		"[youtube] abc: Video unavailable":                                                        "not available",
		"Requested format is not available. Use --list-formats":                                   "quality or format",
		"Could not copy Chrome cookie database. See https://github.com/yt-dlp/yt-dlp/issues/7271": "cookies",
		"unable to download video data: HTTP Error 403: Forbidden":                                "refused",
		"[Errno 28] No space left on device":                                                      "disk is full",
	}
	for text, want := range cases {
		err := explainYtDlp([]string{text}, errors.New("exit 1"))
		var ue *UserError
		if !errors.As(err, &ue) || !strings.Contains(ue.Msg, want) || ue.Hint == "" {
			t.Errorf("%q: %v", text, err)
		}
		if !strings.Contains(ue.Err.Error(), text) {
			t.Errorf("yt-dlp's own words are lost: %v", ue.Err)
		}
	}
	err := explainYtDlp([]string{"something entirely new"}, nil)
	if !strings.Contains(err.Error(), "something entirely new") {
		t.Errorf("unknown error hides what yt-dlp said: %v", err)
	}
}
