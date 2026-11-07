// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Downloads of the programs VideoCatcher needs (yt-dlp, FFmpeg, Deno), with
// the rule of dev-starterkit 08-sicherheit.md S4: whatever is downloaded and
// then run is checked before it is used. Written into ".part", checked,
// then renamed; a file that does not match never takes the place of one
// that works. Taken from DupFinder 1.2.0 and extended by an upper size limit
// for files whose exact size is not known in advance (yt-dlp).

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// pinnedFile is one download that is accepted only if its SHA-256 matches.
// Size > 0 must match exactly; Size == 0 means "not known in advance", then
// MaxSize limits what is read.
type pinnedFile struct {
	Name    string // shown to the user, e.g. "FFmpeg n9.0.2"
	URL     string
	Size    int64
	MaxSize int64
	SHA256  string // lower case hex
}

// userAgent names the program, nothing about the user (08 S12).
const userAgent = "VideoCatcher (+https://github.com/burnersen/VideoCatcher)"

// newHTTPClient never hangs forever: every phase has a limit. The body has no
// overall limit (large files on slow lines); cancel the context to stop.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   30 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}
}

// fetchPinned tries the urls in order until one delivers the pinned file.
// The error of the last source is returned, the others are logged.
func fetchPinned(ctx context.Context, client *http.Client, logger *slog.Logger, f pinnedFile, urls []string, dest string, show func(done, total int64)) error {
	err := errors.New("no download source")
	for _, u := range urls {
		f.URL = u
		if err = downloadVerified(ctx, client, f, dest, show); err == nil {
			logger.Info("downloaded", "name", f.Name, "url", u) // release URLs have no query part
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		logger.Warn("download failed, trying the next source", "url", u, "err", err)
	}
	return err
}

// downloadVerified fetches f into dest. It writes to dest.part first and
// renames only after size and checksum matched: dest is either the complete,
// checked file or untouched. show, if not nil, is told about the progress.
func downloadVerified(ctx context.Context, client *http.Client, f pinnedFile, dest string, show func(done, total int64)) (err error) {
	limit := f.Size
	if limit <= 0 {
		limit = f.MaxSize
	}
	if limit <= 0 {
		return fmt.Errorf("download %s: neither size nor upper limit given", f.Name)
	}
	resp, err := httpGet(ctx, client, f.URL, f.Name)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	part := dest + ".part"
	out, err := os.Create(part)
	if err != nil {
		return fileError(err, "create", part)
	}
	defer func() {
		out.Close() // ignore: closed below on success; this only cleans up
		if err != nil {
			os.Remove(part) // ignore: a broken download never stays behind
		}
	}()

	total := f.Size
	if total <= 0 {
		total = resp.ContentLength
	}
	var body io.Reader = resp.Body
	if show != nil {
		body = &progressReader{reader: resp.Body, total: total, show: show}
	}
	h := sha256.New()
	// Read one byte more than allowed, so a larger file is noticed.
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return netError(err, f.Name)
	}
	if (f.Size > 0 && n != f.Size) || n > limit || n == 0 {
		return userError(fmt.Errorf("got %d bytes, expected %d (limit %d)", n, f.Size, limit),
			"The download of "+f.Name+" is incomplete or not the expected file",
			"Nothing was installed. Try again; if it keeps failing, please report it.")
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, f.SHA256) {
		return userError(fmt.Errorf("SHA-256 %s, expected %s", got, f.SHA256),
			"The download of "+f.Name+" does not match its checksum",
			"Nothing was installed. The file was damaged on the way or changed on the server. Please report it.")
	}
	if err = out.Sync(); err != nil {
		return fileError(err, "write", part)
	}
	if err = out.Close(); err != nil {
		return fileError(err, "write", part)
	}
	if err = os.Rename(part, dest); err != nil {
		return fileError(err, "save", dest)
	}
	return nil
}

// fetchSmall reads a small text file (checksum list, signature) into memory,
// at most limit bytes.
func fetchSmall(ctx context.Context, client *http.Client, url, what string, limit int64) ([]byte, error) {
	resp, err := httpGet(ctx, client, url, what)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, netError(err, what)
	}
	if int64(len(data)) > limit {
		return nil, userError(fmt.Errorf("%s is larger than %d bytes", url, limit),
			"Cannot download "+what+": the server sent something unexpected", "Try again later.")
	}
	return data, nil
}

func httpGet(ctx context.Context, client *http.Client, url, what string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", what, err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, netError(err, what)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close() // ignore: the status is the error
		return nil, userError(fmt.Errorf("HTTP %s from %s", resp.Status, hostOf(url)),
			"Cannot download "+what, "The server refused it right now. Try again later.")
	}
	return resp, nil
}

// netError explains the usual network problems. The technical text stays
// in the error for the log.
func netError(err error, what string) error {
	var dnsErr *net.DNSError
	var certErr *x509.UnknownAuthorityError
	var netErr net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return err
	case errors.As(err, &dnsErr):
		return userError(err, "Cannot download "+what+": the server name was not found",
			"Check the internet connection.")
	case errors.As(err, &certErr):
		return userError(err, "Cannot download "+what+": the connection is not trusted",
			"A virus scanner or company proxy may be intercepting it. Try another network.")
	case errors.As(err, &netErr) && netErr.Timeout():
		return userError(err, "Cannot download "+what+": the server does not answer",
			"Check the internet connection and try again.")
	default:
		return userError(err, "Cannot download "+what,
			"Check the internet connection and try again.")
	}
}

func hostOf(url string) string {
	rest := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	host, _, _ := strings.Cut(rest, "/")
	return host
}

// progressReader counts the bytes read and reports them at most every 200 ms,
// so a long download does not look like a hang.
type progressReader struct {
	reader    io.Reader
	total     int64
	read      int64
	lastPrint time.Time
	show      func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.reader.Read(b)
	p.read += int64(n)
	if time.Since(p.lastPrint) >= 200*time.Millisecond || err == io.EOF {
		p.show(p.read, p.total)
		p.lastPrint = time.Now()
	}
	return n, err
}
