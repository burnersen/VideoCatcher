// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeJSONIsValid(t *testing.T) {
	rt, err := loadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"ffmpeg", "deno"} {
		for _, p := range []string{"windows/amd64", "windows/arm64"} {
			if _, ok := rt.runtimeFor(tool, p); !ok {
				t.Errorf("runtime.json has no %s for %s", tool, p)
			}
		}
	}
}

// fakeRelease serves what GitHub serves for one yt-dlp release.
type fakeRelease struct {
	tag     string
	program []byte // the yt-dlp program for this computer
	sums    []byte
	sig     []byte
	served  int // downloads of the program
}

func newFakeRelease(t *testing.T, key testKey, tag string, program []byte) *fakeRelease {
	asset, err := ytdlpAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	sum := sha256.Sum256(program)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + asset + "\n" + strings.Repeat("0", 64) + "  yt-dlp.tar.gz\n")
	return &fakeRelease{tag: tag, program: program, sums: sums, sig: signForTest(t, key, sums, true)}
}

func (r *fakeRelease) serve(t *testing.T) {
	asset, _ := ytdlpAsset(runtime.GOOS, runtime.GOARCH) // ignore: checked in newFakeRelease
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		dl := "/releases/download/" + r.tag + "/"
		switch req.URL.Path {
		case "/releases/latest":
			http.Redirect(w, req, "/releases/tag/"+r.tag, http.StatusFound)
		case dl + "SHA2-256SUMS":
			w.Write(r.sums)
		case dl + "SHA2-256SUMS.sig":
			w.Write(r.sig)
		case dl + asset:
			r.served++
			w.Write(r.program)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	oldRepo, oldKey := ytdlpRepo, ytdlpTrustedKey
	t.Cleanup(func() { ytdlpRepo, ytdlpTrustedKey = oldRepo, oldKey })
	ytdlpRepo = srv.URL
}

func trustKey(k testKey) { ytdlpTrustedKey = func() (pgpKey, error) { return k.key, nil } }

func runUpdate(t *testing.T, ts toolSet, st *toolsState) (ytdlpUpdate, error) {
	t.Helper()
	return updateYtDlp(context.Background(), newHTTPClient(), slog.New(slog.DiscardHandler), ts, st, nil)
}

func TestYtDlpUpdateInstallsASignedRelease(t *testing.T) {
	key := newTestKey(t)
	rel := newFakeRelease(t, key, "2026.11.01", []byte("new yt-dlp"))
	rel.serve(t)
	trustKey(key)
	ts := newToolSet(t.TempDir())
	st := toolsState{Tools: map[string]toolEntry{}}

	res, err := runUpdate(t, ts, &st)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Version != "2026.11.01" {
		t.Fatalf("%+v", res)
	}
	if got, _ := os.ReadFile(ts.YtDlp); string(got) != "new yt-dlp" { // ignore: compared right here
		t.Fatalf("installed %q", got)
	}

	// Same version and untouched file: nothing is downloaded again.
	if res, err := runUpdate(t, ts, &st); err != nil || res.Changed || rel.served != 1 {
		t.Fatalf("second check: %+v %v, downloads %d", res, err, rel.served)
	}

	// A changed file (damaged, or replaced by something else) is replaced.
	if err := os.WriteFile(ts.YtDlp, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if res, err := runUpdate(t, ts, &st); err != nil || !res.Changed {
		t.Fatalf("after tampering: %+v %v", res, err)
	}
	if got, _ := os.ReadFile(ts.YtDlp); string(got) != "new yt-dlp" { // ignore: compared right here
		t.Fatalf("after tampering: %q", got)
	}
}

// The promise of the README: a yt-dlp that is not signed by the yt-dlp key,
// or that does not match the signed list, never replaces the working one.
func TestYtDlpUpdateNeverInstallsUncheckedFiles(t *testing.T) {
	key := newTestKey(t)
	cases := map[string]func(r *fakeRelease){
		"signature by another key": func(r *fakeRelease) { r.sig = signForTest(t, newTestKey(t), r.sums, false) },
		"list changed after signing": func(r *fakeRelease) {
			r.sums = bytes.Replace(r.sums, []byte("yt-dlp.tar.gz"), []byte("yt-dlp.tar.xz"), 1)
		},
		"program not the one in the list": func(r *fakeRelease) { r.program = []byte("evil yt-dlp") },
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			rel := newFakeRelease(t, key, "2026.11.01", []byte("new yt-dlp"))
			spoil(rel)
			rel.serve(t)
			trustKey(key)
			ts := newToolSet(t.TempDir())
			if err := os.MkdirAll(ts.Dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(ts.YtDlp, []byte("old yt-dlp"), 0o755); err != nil {
				t.Fatal(err)
			}
			st := toolsState{Tools: map[string]toolEntry{}}
			if _, err := runUpdate(t, ts, &st); err == nil {
				t.Fatal("accepted")
			}
			if got, _ := os.ReadFile(ts.YtDlp); string(got) != "old yt-dlp" { // ignore: compared right here
				t.Fatalf("the working yt-dlp was replaced by %q", got)
			}
			if _, ok := st.Tools["yt-dlp"]; ok {
				t.Fatal("state records an unchecked yt-dlp")
			}
			left, _ := filepath.Glob(filepath.Join(ts.Dir, "*.part")) // ignore: pattern is constant
			if len(left) > 0 {
				t.Fatalf("left behind: %v", left)
			}
		})
	}
}

func TestLatestTagIsChecked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/releases/tag/../../evil", http.StatusFound)
	}))
	defer srv.Close()
	old := ytdlpRepo
	defer func() { ytdlpRepo = old }()
	ytdlpRepo = srv.URL
	if tag, err := latestYtDlpTag(context.Background(), newHTTPClient()); err == nil {
		t.Fatalf("accepted tag %q", tag)
	}
}

func TestParseSums(t *testing.T) {
	list := []byte("AAAA  x\n" + strings.Repeat("ab", 32) + " *yt-dlp.exe\n")
	if got, err := parseSums(list, "yt-dlp.exe"); err != nil || got != strings.Repeat("ab", 32) {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := parseSums(list, "x"); err == nil {
		t.Fatal("a short checksum was accepted")
	}
	if _, err := parseSums(list, "missing"); err == nil {
		t.Fatal("missing file found")
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallPinnedUnpacksOnlyThePrograms(t *testing.T) {
	archive := makeZip(t, map[string]string{
		"ffmpeg-x/bin/ffmpeg.exe":  "ffmpeg",
		"ffmpeg-x/bin/ffprobe.exe": "ffprobe",
		"ffmpeg-x/bin/ffplay.exe":  "ffplay",
		"ffmpeg-x/LICENSE.txt":     "GPL",
	})
	sum := sha256.Sum256(archive)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/mirror/ff.zip" {
			http.NotFound(w, req) // the mirror is empty: the next source is used
			return
		}
		w.Write(archive)
	}))
	defer srv.Close()
	e := runtimeEntry{Tool: "ffmpeg", Name: "FFmpeg test", File: "ff.zip", Size: int64(len(archive)),
		SHA256: hex.EncodeToString(sum[:]), Extract: []string{"bin/ffmpeg.exe", "bin/ffprobe.exe"},
		URLs: []string{srv.URL + "/mirror/ff.zip", srv.URL + "/origin/ff.zip"}}
	ts := newToolSet(t.TempDir())
	st := toolsState{Tools: map[string]toolEntry{}}
	if err := installPinned(context.Background(), newHTTPClient(), slog.New(slog.DiscardHandler), ts, &st, e, nil); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(ts.Dir) // ignore: checked by the length below
	var names []string
	for _, en := range entries {
		names = append(names, en.Name())
	}
	if strings.Join(names, ",") != "ffmpeg.exe,ffprobe.exe" {
		t.Fatalf("bin folder holds %v", names)
	}
	if !ts.intact(st.Tools["ffmpeg"]) {
		t.Fatal("state does not match the unpacked files")
	}

	// A wrong checksum: nothing is unpacked.
	e.SHA256 = strings.Repeat("0", 64)
	ts2 := newToolSet(t.TempDir())
	if err := installPinned(context.Background(), newHTTPClient(), slog.New(slog.DiscardHandler), ts2, &st, e, nil); err == nil {
		t.Fatal("wrong checksum accepted")
	}
	if entries, _ := os.ReadDir(ts2.Dir); len(entries) != 0 { // ignore: an empty list is the point
		t.Fatalf("left behind: %v", entries)
	}
}

func TestToolsStateRoundTripAndNewerFormat(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tools.json")
	st := toolsState{Tools: map[string]toolEntry{"deno": {Version: "Deno 2", Files: map[string]string{"deno.exe": "ab"}}}}
	if err := saveToolsState(p, st); err != nil {
		t.Fatal(err)
	}
	got, err := loadToolsState(p)
	if err != nil || got.Tools["deno"].Version != "Deno 2" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := os.WriteFile(p, []byte(`{"format": 99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadToolsState(p); err == nil || !strings.Contains(err.Error(), "newer version") {
		t.Fatalf("newer format: %v", err)
	}
	if err := os.WriteFile(p, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := loadToolsState(p); err != nil || len(got.Tools) != 0 {
		t.Fatalf("broken file: %+v %v", got, err)
	}
}
