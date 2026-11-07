// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsNativeHostStart(t *testing.T) {
	for _, args := range [][]string{
		{"chrome-extension://" + chromeExtensionID + "/"},
		{"chrome-extension://" + chromeExtensionID + "/", "--parent-window=0"},
		{`D:\Tools\VideoCatcher\data\native\de.burnersen.videocatcher.firefox.json`, firefoxExtensionID},
		{"--native-host"},
	} {
		if !isNativeHostStart(args) {
			t.Errorf("%q not seen as a browser start", args)
		}
	}
	for _, args := range [][]string{nil, {"--add", "https://x.org"}, {"videocatcher://add?url=x"}, {"--request", "abcdefgh.json"}} {
		if isNativeHostStart(args) {
			t.Errorf("%q seen as a browser start", args)
		}
	}
}

func message(t *testing.T, v any) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := writeNativeMessage(&b, v); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func replies(t *testing.T, out []byte) []nativeReply {
	t.Helper()
	r := bytes.NewReader(out)
	var list []nativeReply
	for {
		data, err := readNativeMessage(r)
		if errors.Is(err, io.EOF) {
			return list
		}
		if err != nil {
			t.Fatal(err)
		}
		var rep nativeReply
		if err := json.Unmarshal(data, &rep); err != nil {
			t.Fatal(err)
		}
		list = append(list, rep)
	}
}

func TestNativeMessageFrame(t *testing.T) {
	b := message(t, map[string]string{"type": "ping"})
	if n := binary.LittleEndian.Uint32(b[:4]); int(n) != len(b)-4 {
		t.Fatalf("length %d for %d bytes", n, len(b)-4)
	}
	for name, in := range map[string][]byte{
		"zero length":       {0, 0, 0, 0},
		"too long":          binary.LittleEndian.AppendUint32(nil, maxNativeMessage+1),
		"cut off":           append(binary.LittleEndian.AppendUint32(nil, 10), '{'),
		"cut off in length": {1, 0},
	} {
		if _, err := readNativeMessage(bytes.NewReader(in)); err == nil || errors.Is(err, io.EOF) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := readNativeMessage(bytes.NewReader(nil)); !errors.Is(err, io.EOF) {
		t.Errorf("empty input must be the normal end, got %v", err)
	}
}

func testHost(t *testing.T) (*nativeHost, *[]string) {
	started := &[]string{}
	h := &nativeHost{
		inbox:  filepath.Join(t.TempDir(), "inbox"),
		start:  func(name string) error { *started = append(*started, name); return nil },
		logger: slog.New(slog.DiscardHandler),
	}
	return h, started
}

func TestNativeHostAdd(t *testing.T) {
	h, started := testHost(t)
	in := append(message(t, nativeMessage{Type: "ping"}), message(t, nativeMessage{
		Type: "add", URL: "https://cdn.example.org/hls/master.m3u8?t=1", PageURL: "https://example.org/watch/1",
		Title: "A <b>title</b>", Referer: "https://player.example.net/e/abc", FileName: "A title",
		Cookies: "# Netscape HTTP Cookie File\n.example.org\tTRUE\t/\tTRUE\t0\tsid\tsecret\n",
	})...)
	var out bytes.Buffer
	if err := h.serve(bytes.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	r := replies(t, out.Bytes())
	if len(r) != 2 || !r[0].OK || r[0].Version != version || !r[1].OK {
		t.Fatalf("replies %+v", r)
	}
	if len(*started) != 1 || !requestName.MatchString((*started)[0]) {
		t.Fatalf("started %v", *started)
	}
	// The window reads the file exactly as the extension sent it.
	in2 := &intake{inbox: h.inbox, cookies: filepath.Join(filepath.Dir(h.inbox), "cookies"), logger: h.logger}
	req, err := in2.readRequest((*started)[0])
	if err != nil {
		t.Fatal(err)
	}
	if req.Source != "extension" || req.URL != "https://cdn.example.org/hls/master.m3u8?t=1" || req.Referer != "https://player.example.net/e/abc" ||
		req.Title != "A <b>title</b>" || req.FileName != "A title" || req.PageURL != "https://example.org/watch/1" {
		t.Errorf("request %+v", req)
	}
	if req.CookieFile == "" {
		t.Fatal("cookies not written")
	}
	if data, _ := os.ReadFile(req.CookieFile); !strings.Contains(string(data), "sid\tsecret") {
		t.Error("cookie file has not the cookies")
	}
	if entries, _ := os.ReadDir(h.inbox); len(entries) != 0 {
		t.Errorf("inbox not empty after reading: %v", entries)
	}
}

func TestNativeHostRefuses(t *testing.T) {
	h, started := testHost(t)
	in := message(t, nativeMessage{Type: "add", URL: "file:///C:/Windows/win.ini"})
	in = append(in, message(t, nativeMessage{Type: "add", URL: "--exec=calc"})...)
	in = append(in, message(t, nativeMessage{Type: "format c:"})...)
	in = append(in, message(t, "not an object")...)
	var out bytes.Buffer
	if err := h.serve(bytes.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	r := replies(t, out.Bytes())
	want := []string{"bad-link", "bad-link", "bad-message", "bad-message"}
	if len(r) != len(want) {
		t.Fatalf("replies %+v", r)
	}
	for i, w := range want {
		if r[i].OK || r[i].Code != w {
			t.Errorf("reply %d: %+v, want code %s", i, r[i], w)
		}
	}
	if len(*started) != 0 {
		t.Errorf("started for a refused link: %v", *started)
	}
	if entries, _ := os.ReadDir(h.inbox); len(entries) != 0 {
		t.Errorf("files for refused links: %v", entries)
	}
	// Bad page and referer links are dropped, the link itself still goes.
	h2, started2 := testHost(t)
	if rep := h2.handle(nativeMessage{Type: "add", URL: "https://x.org/v.mp4", Referer: "javascript:alert(1)", PageURL: "nope"}); !rep.OK {
		t.Fatalf("%+v", rep)
	}
	in2 := &intake{inbox: h2.inbox, cookies: t.TempDir(), logger: h2.logger}
	req, err := in2.readRequest((*started2)[0])
	if err != nil || req.Referer != "" || req.PageURL != "" {
		t.Errorf("%+v %v", req, err)
	}
}

// A window that cannot start does not lose the link: the request waits in
// the inbox for the next start.
func TestNativeHostStartFails(t *testing.T) {
	h, _ := testHost(t)
	h.start = func(string) error { return errors.New("no") }
	rep := h.handle(nativeMessage{Type: "add", URL: "https://x.org/v"})
	if rep.OK || rep.Code != "start" {
		t.Fatalf("%+v", rep)
	}
	if entries, _ := os.ReadDir(h.inbox); len(entries) != 1 || !requestName.MatchString(entries[0].Name()) {
		t.Errorf("request not kept: %v", entries)
	}
}

func TestNativeHostBrokenStream(t *testing.T) {
	h, _ := testHost(t)
	var out bytes.Buffer
	if err := h.serve(bytes.NewReader([]byte{0xff, 0xff, 0xff, 0x7f}), &out); err == nil {
		t.Fatal("a broken stream must end serve with an error")
	}
	if r := replies(t, out.Bytes()); len(r) != 1 || r[0].Code != "bad-message" {
		t.Errorf("%+v", r)
	}
}

func TestNativeManifests(t *testing.T) {
	exe := `C:\Tools\VideoCatcher.exe`
	c, f, err := nativeManifests(exe)
	if err != nil {
		t.Fatal(err)
	}
	var mc, mf hostManifest
	if json.Unmarshal(c, &mc) != nil || json.Unmarshal(f, &mf) != nil {
		t.Fatal("not JSON")
	}
	if mc.Name != nativeHostName || mc.Path != exe || mc.Type != "stdio" ||
		len(mc.AllowedOrigins) != 1 || mc.AllowedOrigins[0] != "chrome-extension://"+chromeExtensionID+"/" || mc.AllowedExtensions != nil {
		t.Errorf("chromium manifest %s", c)
	}
	if mf.Name != nativeHostName || len(mf.AllowedExtensions) != 1 || mf.AllowedExtensions[0] != firefoxExtensionID || mf.AllowedOrigins != nil {
		t.Errorf("firefox manifest %s", f)
	}
	dir := t.TempDir()
	if err := writeManifests(dir, exe); err != nil {
		t.Fatal(err)
	}
	cp, fp := manifestFiles(dir)
	if !manifestPointsTo(cp, exe) || !manifestPointsTo(fp, `c:\tools\videocatcher.exe`) {
		t.Error("written manifests do not point to the program")
	}
	if manifestPointsTo(cp, `D:\Other\VideoCatcher.exe`) || manifestPointsTo(filepath.Join(dir, "missing.json"), exe) {
		t.Error("a manifest for another place counts as current")
	}
}

// The Chrome extension's ID comes from the public key in its manifest. If
// the key changes, the helper would refuse the extension.
func TestExtensionIDMatchesKey(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("extension", "src", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Key      string `json:"key"`
		Settings struct {
			Gecko struct {
				ID string `json:"id"`
			} `json:"gecko"`
		} `json:"browser_specific_settings"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	der, err := base64.StdEncoding.DecodeString(m.Key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	id := []byte(hex.EncodeToString(sum[:16]))
	for i, c := range id { // Chrome writes the hex digits 0-f as the letters a-p
		if c <= '9' {
			id[i] = 'a' + c - '0'
		} else {
			id[i] = 'a' + 10 + c - 'a'
		}
	}
	if string(id) != chromeExtensionID {
		t.Errorf("key gives %s, the helper allows %s", id, chromeExtensionID)
	}
	if m.Settings.Gecko.ID != firefoxExtensionID {
		t.Errorf("gecko id %q, the helper allows %q", m.Settings.Gecko.ID, firefoxExtensionID)
	}
}

func TestSafeFileName(t *testing.T) {
	for in, want := range map[string]string{
		"A video: part 1/2 | HD":          "A video part 1 2 HD",
		"  Watch   this?  ":               "Watch this",
		"Title...":                        "Title",
		"line\nbreak\ttab":                "line break tab",
		"CON":                             "",
		"":                                "",
		"???":                             "",
		"Über Ärger 100% (2026) - Teil 2": "Über Ärger 100% (2026) - Teil 2",
	} {
		if got := safeFileName(in); got != want {
			t.Errorf("safeFileName(%q) = %q, want %q", in, got, want)
		}
	}
	long := safeFileName(strings.Repeat("ä", 300))
	if long == "" || len(long) > maxFileNameLen {
		t.Errorf("long title gives %d bytes", len(long))
	}
	o := DownloadOptions{FileName: long, Playlist: "single"}
	if err := o.checkFileName(); err != nil {
		t.Errorf("cleaned long name refused: %v", err)
	}
}
