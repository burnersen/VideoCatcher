// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		current, latest string
		want            bool
	}{
		{"0.4.0", "v0.5.0", true},
		{"0.4.0", "v0.4.1", true},
		{"0.9.9", "v0.10.0", true}, // numbers, not text
		{"0.4.0", "v1.0.0", true},
		{"0.4.0", "v0.4.0", false},
		{"0.5.0", "v0.4.9", false},
		{"dev", "v0.5.0", false},
		{"0.4.0", "v0.5.0-beta", false},
		{"0.4.0", "nightly", false},
		{"0.4.0", "", false},
		{"0.4.0", "v0.5", false},
	} {
		if got := newerVersion(c.current, c.latest); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v", c.current, c.latest, got)
		}
	}
}

func TestNewerAppReadsTheRedirect(t *testing.T) {
	location := "/burnersen/VideoCatcher/releases/tag/v0.5.0"
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		asked++
		if location == "" {
			http.NotFound(w, req) // a private repository, or no release yet
			return
		}
		http.Redirect(w, req, location, http.StatusFound)
	}))
	defer srv.Close()
	old := appRepo
	defer func() { appRepo = old }()
	appRepo = srv.URL
	ctx, client := context.Background(), newHTTPClient()

	if v, err := newerApp(ctx, client, "0.4.0"); err != nil || v != "0.5.0" {
		t.Fatalf("%q %v", v, err)
	}
	if got := releasePage("0.5.0"); got != srv.URL+"/releases/tag/v0.5.0" {
		t.Fatal(got)
	}
	if v, err := newerApp(ctx, client, "0.5.0"); err != nil || v != "" {
		t.Fatalf("the same version: %q %v", v, err)
	}
	// A tag that is no version never reaches the window or a link.
	location = "/releases/tag/v9.9.9/../../evil"
	if v, err := newerApp(ctx, client, "0.4.0"); err != nil || v != "" {
		t.Fatalf("odd tag: %q %v", v, err)
	}
	location = ""
	if v, err := newerApp(ctx, client, "0.4.0"); err != nil || v != "" {
		t.Fatalf("no release: %q %v", v, err)
	}
	// A local build asks nobody.
	before := asked
	if v, err := newerApp(ctx, client, "dev"); err != nil || v != "" || asked != before {
		t.Fatalf("dev: %q %v, asked %d times", v, err, asked-before)
	}
}
