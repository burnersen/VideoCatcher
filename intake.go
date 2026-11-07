// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// How links reach VideoCatcher from outside its window. There are three
// ways, and all end in the same function (App.accept):
//
//	VideoCatcher.exe --add <link>          a shortcut, a script, "Send to"
//	VideoCatcher.exe videocatcher://add?url=<link>&title=...
//	                                       a link on a web page or a
//	                                       bookmarklet (after the user turned
//	                                       browser links on in the settings)
//	VideoCatcher.exe --request <name>      the browser extension (later): its
//	                                       helper writes a request file into
//	                                       the inbox and starts VideoCatcher
//	                                       with the file's name
//
// If VideoCatcher already runs, the new start only hands its arguments to
// the running window (Wails single instance lock) and ends. A request file
// keeps cookies off the command line, where other programs could read them
// (08 S3). Its format is described in docs/browser-bridge.md.
//
// Links from a web page are never trusted: they wait in the list until the
// user clicks OK (queue.go, jobConfirm).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// intakeRequest is one link from outside, in the format of a request file
// (version 1).
type intakeRequest struct {
	V          int    `json:"v"`
	URL        string `json:"url"`
	PageURL    string `json:"pageUrl,omitempty"`
	Title      string `json:"title,omitempty"`
	Referer    string `json:"referer,omitempty"`
	Source     string `json:"source"`            // extension, cli, protocol
	Cookies    string `json:"cookies,omitempty"` // Netscape cookie file text, only from the extension
	CookieFile string `json:"-"`                 // where the cookies were written for yt-dlp
}

const (
	requestVersion  = 1
	maxRequestBytes = 1 << 20 // a link, a title and the cookies of one site
	maxTitleLen     = 300
	protocolScheme  = "videocatcher"
)

var requestName = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}\.json$`)

type intake struct {
	inbox   string // request files of the browser helper
	cookies string // cookie files for yt-dlp, one per job
	logger  *slog.Logger
}

func newIntake(dataDir string, logger *slog.Logger) *intake {
	return &intake{inbox: filepath.Join(dataDir, "inbox"), cookies: filepath.Join(dataDir, "cookies"), logger: logger}
}

// handleArgs reads the arguments of a start (this one or a second one).
// Unknown arguments are ignored: Windows and Wails add their own.
func (in *intake) handleArgs(args []string, accept func(intakeRequest)) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case (arg == "--add" || arg == "-add") && i+1 < len(args):
			i++
			accept(intakeRequest{V: requestVersion, URL: args[i], Source: "cli"})
		case (arg == "--request" || arg == "-request") && i+1 < len(args):
			i++
			req, err := in.readRequest(args[i])
			if err != nil {
				in.logger.Warn("request refused", "name", args[i], "err", err)
				continue
			}
			accept(req)
		case strings.HasPrefix(strings.ToLower(arg), protocolScheme+":"):
			req, err := parseProtocolLink(arg)
			if err != nil {
				in.logger.Warn("protocol link refused", "err", err)
				continue
			}
			accept(req)
		}
	}
}

// parseProtocolLink reads videocatcher://add?url=...&title=...&referer=...
func parseProtocolLink(raw string) (intakeRequest, error) {
	if len(raw) > maxURLLen*2 {
		return intakeRequest{}, errors.New("link too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return intakeRequest{}, fmt.Errorf("read %s link: %w", protocolScheme, err)
	}
	action := strings.Trim(u.Host+u.Opaque+u.Path, "/")
	if !strings.EqualFold(u.Scheme, protocolScheme) || action != "add" {
		return intakeRequest{}, fmt.Errorf("unknown %s action %q", protocolScheme, action)
	}
	q := u.Query()
	return intakeRequest{V: requestVersion, URL: q.Get("url"), Title: clip(q.Get("title"), maxTitleLen),
		Referer: q.Get("referer"), Source: "protocol"}, nil
}

// readRequest reads and removes one request file from the inbox. Only plain
// names inside the inbox are accepted, never a path.
func (in *intake) readRequest(name string) (intakeRequest, error) {
	if !requestName.MatchString(name) {
		return intakeRequest{}, fmt.Errorf("not a request name: %q", name)
	}
	p := filepath.Join(in.inbox, name)
	data, err := readLimited(p, maxRequestBytes)
	os.Remove(p) // ignore: a request is used once; if this fails, watch tries again and fails the same
	if err != nil {
		return intakeRequest{}, err
	}
	var req intakeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return intakeRequest{}, fmt.Errorf("read request: %w", err)
	}
	if req.V != requestVersion {
		return intakeRequest{}, fmt.Errorf("request version %d, expected %d", req.V, requestVersion)
	}
	if req.Source != "extension" {
		req.Source = "protocol" // anything that does not say where it is from waits for the user
	}
	req.Title = clip(req.Title, maxTitleLen)
	if req.Cookies != "" {
		if req.CookieFile, err = in.writeCookies(req.Cookies); err != nil {
			in.logger.Warn("cookies not used", "err", err) // the link still goes, only without login
		}
		req.Cookies = ""
	}
	return req, nil
}

// writeCookies stores the cookies of one request in a file only this user
// may read; the job removes it when it ends.
func (in *intake) writeCookies(text string) (string, error) {
	if !strings.HasPrefix(text, "# Netscape HTTP Cookie File") && !strings.HasPrefix(text, "# HTTP Cookie File") {
		return "", errors.New("cookies are not in the Netscape format")
	}
	if err := os.MkdirAll(in.cookies, 0o700); err != nil {
		return "", fmt.Errorf("create cookie folder: %w", err)
	}
	f, err := os.CreateTemp(in.cookies, "c-*.txt")
	if err != nil {
		return "", fmt.Errorf("create cookie file: %w", err)
	}
	if err := f.Chmod(0o600); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		in.logger.Debug("chmod cookie file", "err", err) // Windows: the folder of the user protects it
	}
	_, werr := f.WriteString(text)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(f.Name()) // ignore: cleanup after the error that is returned
		return "", fmt.Errorf("write cookie file: %w", werr)
	}
	return f.Name(), nil
}

// watch handles request files that were left in the inbox (a start of
// VideoCatcher that did not reach the window), at start and then every few
// seconds. Old cookie files of jobs that never ran are removed at start.
func (in *intake) watch(ctx context.Context, accept func(intakeRequest)) {
	in.removeOldCookies()
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		entries, err := os.ReadDir(in.inbox)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			in.logger.Warn("cannot read the inbox", "err", err)
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || !requestName.MatchString(e.Name()) {
				continue
			}
			req, err := in.readRequest(e.Name())
			if err != nil {
				in.logger.Warn("request refused", "name", e.Name(), "err", err)
				continue
			}
			accept(req)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// removeOldCookies deletes cookie files older than a day: their job is
// gone (VideoCatcher ended in between, or the job was removed).
func (in *intake) removeOldCookies() {
	entries, err := os.ReadDir(in.cookies)
	if err != nil {
		return // no folder, nothing to do
	}
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && time.Since(info.ModTime()) > 24*time.Hour {
			os.Remove(filepath.Join(in.cookies, e.Name())) // ignore: tried again next start
		}
	}
}

func readLimited(p string, limit int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close() // read only: a close error changes nothing
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", p, limit)
	}
	return data, nil
}

// clip shortens s to at most n bytes, without cutting a character in half.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "..."
}
