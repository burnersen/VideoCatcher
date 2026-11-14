// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// The helper for the browser extension (native messaging). The browser
// starts VideoCatcher.exe itself, only for the extension named in the
// manifest below, and talks to it through stdin and stdout: each message is
// 4 bytes of length (little endian, the order of x86 and ARM) and then JSON.
// There is no network port a web page could reach.
//
// The helper does not open a window. For a link it writes a request file
// into the inbox and starts "VideoCatcher.exe --request <name>"; a running
// window takes it over (single instance lock), otherwise the window starts.
// The request format is the one in intake.go and docs/browser-bridge.md.
//
// How the browser starts it (no way to add our own argument):
//
//	Chrome, Edge:  VideoCatcher.exe chrome-extension://<id>/ [--parent-window=<n>]
//	Firefox:       VideoCatcher.exe <path of the manifest> videocatcher@burnersen.de

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const (
	// nativeHostName is the name the extension asks for. Lower case letters,
	// digits, dots and underscores only (Chrome's rule).
	nativeHostName = "de.burnersen.videocatcher"
	// chromeExtensionID follows from the public key in the extension's
	// manifest ("key"), the same in Chrome and Edge. The test
	// TestExtensionIDMatchesKey checks that both still fit together.
	chromeExtensionID = "lmloindgceipinokjbodjgcbfmihmlhd"
	// firefoxExtensionID is browser_specific_settings.gecko.id.
	firefoxExtensionID = "videocatcher@burnersen.de"

	// Chrome allows at most 1 MB from the helper to the extension; to the
	// helper up to 64 MB, but a link, a title and the cookies of one site
	// fit in the size a request file may have.
	maxNativeMessage = maxRequestBytes
	// A browser sends one message per start (sendNativeMessage). More than
	// a few in one start is not the extension.
	maxNativeMessages = 20
)

// isNativeHostStart says whether a browser started this program as the
// extension's helper.
func isNativeHostStart(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "chrome-extension://") || a == firefoxExtensionID || a == "--native-host" {
			return true
		}
	}
	return false
}

// nativeMessage is what the extension sends.
type nativeMessage struct {
	Type     string `json:"type"` // ping, add
	URL      string `json:"url"`
	PageURL  string `json:"pageUrl"`
	Title    string `json:"title"`
	Referer  string `json:"referer"`
	FileName string `json:"fileName"` // a name for the file, from the page title; may be empty
	Cookies  string `json:"cookies"`  // Netscape format, only when the user asked for it
	// Headers the browser sent for the video (headers.go); checked by the
	// window when it reads the request.
	Headers map[string]string `json:"headers"`
}

// nativeReply is what the extension gets back.
type nativeReply struct {
	OK      bool   `json:"ok"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"` // in English; the extension has its own words for the common cases
	Code    string `json:"code,omitempty"`  // bad-message, bad-link, inbox, start
}

type nativeHost struct {
	inbox  string
	start  func(requestName string) error // starts VideoCatcher.exe --request <name>
	logger *slog.Logger
}

// serve answers messages until the browser closes stdin.
func (h *nativeHost) serve(r io.Reader, w io.Writer) error {
	for i := 0; i < maxNativeMessages; i++ {
		data, err := readNativeMessage(r)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			// The stream is out of step now; answer once and stop.
			writeNativeMessage(w, nativeReply{Error: err.Error(), Code: "bad-message"}) // ignore: the browser may be gone already
			return err
		}
		var m nativeMessage
		reply := nativeReply{}
		if err := json.Unmarshal(data, &m); err != nil {
			reply = nativeReply{Error: "the message is not JSON", Code: "bad-message"}
		} else {
			reply = h.handle(m)
		}
		if err := writeNativeMessage(w, reply); err != nil {
			return err
		}
	}
	return fmt.Errorf("more than %d messages in one start", maxNativeMessages)
}

func (h *nativeHost) handle(m nativeMessage) nativeReply {
	switch m.Type {
	case "ping":
		return nativeReply{OK: true, Version: version}
	case "add":
		name, err := h.add(m)
		if err != nil {
			h.logger.Warn("extension link refused", "err", err)
			var ne nativeError
			if errors.As(err, &ne) {
				return nativeReply{Error: ne.Error(), Code: ne.code}
			}
			return nativeReply{Error: err.Error(), Code: "inbox"}
		}
		h.logger.Info("extension link", "request", name)
		return nativeReply{OK: true, Version: version}
	}
	return nativeReply{Error: fmt.Sprintf("unknown message type %q", m.Type), Code: "bad-message"}
}

type nativeError struct {
	code string
	err  error
}

func (e nativeError) Error() string { return e.err.Error() }
func (e nativeError) Unwrap() error { return e.err }

// add writes the request file and starts the window with it.
func (h *nativeHost) add(m nativeMessage) (string, error) {
	link, err := checkURL(m.URL)
	if err != nil {
		return "", nativeError{"bad-link", err}
	}
	req := intakeRequest{
		V: requestVersion, URL: link, Source: "extension",
		PageURL: optionalURL(m.PageURL), Referer: optionalURL(m.Referer),
		Title: clip(m.Title, maxTitleLen), FileName: clip(m.FileName, maxFileNameLen), Cookies: m.Cookies,
		Headers: m.Headers,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("encode request: %w", err)
	}
	if len(data) > maxRequestBytes {
		return "", nativeError{"bad-message", fmt.Errorf("request has %d bytes, at most %d", len(data), maxRequestBytes)}
	}
	if err := os.MkdirAll(h.inbox, 0o700); err != nil {
		return "", fmt.Errorf("create inbox: %w", err)
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("make a request name: %w", err)
	}
	name := hex.EncodeToString(id[:]) + ".json"
	// Atomic: the window watches the inbox and must never read half a file.
	if err := writeFileAtomic(filepath.Join(h.inbox, name), data, 0o600); err != nil {
		return "", err
	}
	if err := h.start(name); err != nil {
		// The file stays: a window that is already running, or the next
		// start, picks it up from the inbox (intake.watch).
		return "", nativeError{"start", fmt.Errorf("start %s: %w", appName, err)}
	}
	return name, nil
}

// optionalURL keeps a page or referer link only if it is a web link.
func optionalURL(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	u, err := checkURL(s)
	if err != nil {
		return ""
	}
	return u
}

func readNativeMessage(r io.Reader) ([]byte, error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, errors.New("message cut off in its length")
		}
		return nil, err // io.EOF: the browser closed the pipe, the normal end
	}
	n := binary.LittleEndian.Uint32(head[:])
	if n == 0 || n > maxNativeMessage {
		return nil, fmt.Errorf("message of %d bytes, expected 1 to %d", n, maxNativeMessage)
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("message cut off: %w", err)
	}
	return data, nil
}

func writeNativeMessage(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode reply: %w", err)
	}
	buf := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(buf, uint32(len(data)))
	copy(buf[4:], data)
	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("write reply: %w", err)
	}
	return nil
}

// --- the manifests that tell the browsers where the helper is ---

type hostManifest struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Path              string   `json:"path"`
	Type              string   `json:"type"`
	AllowedOrigins    []string `json:"allowed_origins,omitempty"`    // Chrome, Edge
	AllowedExtensions []string `json:"allowed_extensions,omitempty"` // Firefox
}

// nativeManifests returns the manifest for Chrome and Edge and the one for
// Firefox. Each allows exactly our extension, nothing else.
func nativeManifests(exe string) (chromium, firefox []byte, err error) {
	base := hostManifest{Name: nativeHostName, Description: displayName + " helper for the browser extension", Path: exe, Type: "stdio"}
	c := base
	c.AllowedOrigins = []string{"chrome-extension://" + chromeExtensionID + "/"}
	f := base
	f.AllowedExtensions = []string{firefoxExtensionID}
	if chromium, err = json.MarshalIndent(c, "", "  "); err != nil {
		return nil, nil, err
	}
	if firefox, err = json.MarshalIndent(f, "", "  "); err != nil {
		return nil, nil, err
	}
	return chromium, firefox, nil
}

// manifestFiles are where VideoCatcher keeps the two manifests.
func manifestFiles(dataDir string) (chromium, firefox string) {
	dir := filepath.Join(dataDir, "native")
	return filepath.Join(dir, nativeHostName+".chromium.json"), filepath.Join(dir, nativeHostName+".firefox.json")
}

// writeManifests writes both manifests for exe.
func writeManifests(dataDir, exe string) error {
	c, f, err := nativeManifests(exe)
	if err != nil {
		return err
	}
	cp, fp := manifestFiles(dataDir)
	if err := writeFileAtomic(cp, append(c, '\n'), 0o644); err != nil {
		return err
	}
	return writeFileAtomic(fp, append(f, '\n'), 0o644)
}

// manifestPointsTo says whether the manifest at p names exe.
func manifestPointsTo(p, exe string) bool {
	data, err := readLimited(p, 64<<10)
	if err != nil {
		return false
	}
	var m hostManifest
	return json.Unmarshal(data, &m) == nil && m.Name == nativeHostName && strings.EqualFold(filepath.Clean(m.Path), filepath.Clean(exe))
}

// runNativeHost is the whole program when a browser started it.
func runNativeHost(paths appPaths, stdin io.Reader, stdout io.Writer) int {
	logger := slog.New(slog.DiscardHandler)
	// A log of its own: the window may be writing (and rotating) its log
	// at the same time.
	if l, closeLog, err := openLog(filepath.Join(filepath.Dir(paths.LogFile), "native-host.log"), slog.LevelInfo); err == nil {
		logger = l
		defer closeLog()
	}
	exe, err := os.Executable()
	if err != nil {
		logger.Error("find own program", "err", err)
		writeNativeMessage(stdout, nativeReply{Error: "cannot find " + appName + " itself", Code: "start"}) // ignore: nothing else to tell
		return 1
	}
	h := &nativeHost{
		inbox:  newIntake(paths.DataDir, logger).inbox,
		start:  func(name string) error { return startDetached(exe, "--request", name) },
		logger: logger,
	}
	if err := h.serve(stdin, stdout); err != nil {
		logger.Warn("native host", "err", err)
		return 1
	}
	return 0
}
