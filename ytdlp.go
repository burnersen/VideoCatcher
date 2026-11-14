// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Keeps yt-dlp up to date without trusting the download blindly.
//
// yt-dlp has to be new: sites change, and an old yt-dlp simply stops working
// for them. So VideoCatcher does not pin one version like FFmpeg, it takes
// the newest stable release. A deliberate exception to 08 S4, made safe like
// this:
//
//  1. The newest release tag comes from the redirect of /releases/latest.
//  2. SHA2-256SUMS and SHA2-256SUMS.sig of exactly that release are fetched.
//  3. The signature must be valid and made by the yt-dlp release key, which
//     is built into VideoCatcher (ytdlp-signing-key.asc) and checked against
//     the fingerprint below. No key, no update.
//  4. yt-dlp.exe must match its line in the signed list.
//  5. Only then it replaces the old one, which stays as yt-dlp.exe.old.
//
// yt-dlp's own "yt-dlp -U" is never used: it would replace the program
// without VideoCatcher having checked it.

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
)

//go:embed ytdlp-signing-key.asc
var ytdlpKeyArmored []byte

// ytdlpKeyFingerprint is the yt-dlp release key, as published in yt-dlp's
// README and public.key (checked with gpg on 2026-11-07 against the
// signature of release 2026.10.26).
const ytdlpKeyFingerprint = "AC0CBBE6848D6A873464AF4E57CF65933B5A7581"

// ytdlpRepo is where releases come from. A variable only so tests can point
// it at a local server.
var ytdlpRepo = "https://github.com/yt-dlp/yt-dlp"

// Size limits for what is read from the release (the real files: checksum
// list 1.6 KB, signature 0.6 KB, yt-dlp.exe 18 MB on 2026-11-07).
const (
	ytdlpSumsLimit = 64 << 10
	ytdlpSigLimit  = 16 << 10
	ytdlpExeLimit  = 300 << 20
)

// yt-dlp tags are dates, optionally with a number: 2026.10.26 or 2026.10.26.1
var ytdlpTagPattern = regexp.MustCompile(`^[0-9]{4}\.[0-9]{2}\.[0-9]{2}(\.[0-9]+)?$`)

// ytdlpTrustedKey is the key updates must be signed with. A variable only so
// tests can sign with a key of their own; the program never changes it.
var ytdlpTrustedKey = ytdlpSigningKey

func ytdlpSigningKey() (pgpKey, error) {
	key, err := parseArmoredKey(ytdlpKeyArmored)
	if err != nil {
		return pgpKey{}, fmt.Errorf("built-in yt-dlp key: %w", err)
	}
	if key.Fingerprint != ytdlpKeyFingerprint {
		return pgpKey{}, fmt.Errorf("built-in yt-dlp key has fingerprint %s, expected %s", key.Fingerprint, ytdlpKeyFingerprint)
	}
	return key, nil
}

// ytdlpAsset is the name of the release file for this computer.
func ytdlpAsset(goos, goarch string) (string, error) {
	switch goos + "/" + goarch {
	case "windows/amd64":
		return "yt-dlp.exe", nil
	case "windows/arm64":
		return "yt-dlp_arm64.exe", nil
	case "linux/amd64":
		return "yt-dlp_linux", nil
	case "linux/arm64":
		return "yt-dlp_linux_aarch64", nil
	case "darwin/amd64", "darwin/arm64":
		return "yt-dlp_macos", nil
	}
	return "", userError(fmt.Errorf("no yt-dlp build for %s/%s", goos, goarch),
		"yt-dlp has no ready-made program for this computer", "")
}

// latestYtDlpTag asks GitHub which release is the newest.
func latestYtDlpTag(ctx context.Context, client *http.Client) (string, error) {
	tag, err := latestReleaseTag(ctx, client, ytdlpRepo, "the yt-dlp version list")
	if err != nil {
		return "", err
	}
	if !ytdlpTagPattern.MatchString(tag) {
		return "", userError(fmt.Errorf("newest yt-dlp release: tag %q", tag),
			"Cannot find out which yt-dlp is the newest", "Try again later. The installed yt-dlp keeps working.")
	}
	return tag, nil
}

// latestReleaseTag reads the tag of the newest release of a GitHub
// repository from the redirect of /releases/latest instead of the API: no
// rate limit, no JSON. The tag is "" when the answer names none (no
// release, a private repository); the caller checks its form.
func latestReleaseTag(ctx context.Context, client *http.Client, repo, what string) (string, error) {
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, repo+"/releases/latest", nil)
	if err != nil {
		return "", fmt.Errorf("ask for the newest release: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := noFollow.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", netError(err, what)
	}
	resp.Body.Close() // ignore: only the header counts
	_, tag, found := strings.Cut(resp.Header.Get("Location"), "/releases/tag/")
	if resp.StatusCode/100 != 3 || !found {
		return "", nil
	}
	return tag, nil
}

// parseSums finds the checksum of one file in a GNU style checksum list
// ("<64 hex>  <name>" per line, a "*" before the name means binary mode).
func parseSums(list []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(list))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			sum := strings.ToLower(fields[0])
			if !sha256Hex.MatchString(sum) {
				return "", fmt.Errorf("checksum of %s is not SHA-256: %q", name, fields[0])
			}
			return sum, nil
		}
	}
	return "", fmt.Errorf("%s is not in the checksum list", name)
}

// ytdlpUpdate is the result of a check.
type ytdlpUpdate struct {
	Version string
	Changed bool // a new yt-dlp was installed
}

// updateYtDlp installs the newest yt-dlp if it is not installed yet, or if
// the installed one is older or was changed since it was checked. The state
// is updated in place and saved by the caller.
func updateYtDlp(ctx context.Context, client *http.Client, logger *slog.Logger, ts toolSet, st *toolsState, show func(done, total int64)) (ytdlpUpdate, error) {
	key, err := ytdlpTrustedKey()
	if err != nil {
		return ytdlpUpdate{}, err // a build mistake, the tests catch it
	}
	asset, err := ytdlpAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return ytdlpUpdate{}, err
	}
	tag, err := latestYtDlpTag(ctx, client)
	if err != nil {
		return ytdlpUpdate{}, err
	}
	cur := st.Tools["yt-dlp"]
	if cur.Version == tag && ts.intact(cur) {
		return ytdlpUpdate{Version: tag}, nil
	}

	base := ytdlpRepo + "/releases/download/" + tag + "/"
	sums, err := fetchSmall(ctx, client, base+"SHA2-256SUMS", "the yt-dlp checksum list", ytdlpSumsLimit)
	if err != nil {
		return ytdlpUpdate{}, err
	}
	sig, err := fetchSmall(ctx, client, base+"SHA2-256SUMS.sig", "the yt-dlp signature", ytdlpSigLimit)
	if err != nil {
		return ytdlpUpdate{}, err
	}
	if err := verifyDetached(key, sums, sig); err != nil {
		return ytdlpUpdate{}, userError(err,
			"yt-dlp "+tag+" was not installed: its signature is not valid",
			"Nothing was changed, the installed yt-dlp keeps working. If this stays, please report it.")
	}
	sum, err := parseSums(sums, asset)
	if err != nil {
		return ytdlpUpdate{}, userError(err, "yt-dlp "+tag+" has no program for this computer",
			"Nothing was changed. Try again after the next yt-dlp release.")
	}

	if err := os.MkdirAll(ts.Dir, 0o755); err != nil {
		return ytdlpUpdate{}, fileError(err, "create the folder", ts.Dir)
	}
	fresh := ts.YtDlp + ".new"
	f := pinnedFile{Name: "yt-dlp " + tag, URL: base + asset, MaxSize: ytdlpExeLimit, SHA256: sum}
	if err := downloadVerified(ctx, client, f, fresh, show); err != nil {
		return ytdlpUpdate{}, err
	}
	if err := replaceKeepingOld(ts.YtDlp, fresh); err != nil {
		os.Remove(fresh) // ignore: cleanup after the error that is returned
		return ytdlpUpdate{}, fileError(err, "install", ts.YtDlp)
	}
	st.Tools["yt-dlp"] = toolEntry{Version: tag, Files: map[string]string{exeName("yt-dlp"): sum}}
	logger.Info("yt-dlp installed", "version", tag, "previous", cur.Version, "sha256", sum)
	return ytdlpUpdate{Version: tag, Changed: true}, nil
}

// replaceKeepingOld puts fresh in place of target. The old target stays as
// target.old, so a broken new version can be undone by hand. Windows allows
// renaming a program that is running, but not overwriting it.
func replaceKeepingOld(target, fresh string) error {
	old := target + ".old"
	if err := os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", old, err)
	}
	if err := os.Rename(target, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("keep the old version: %w", err)
	}
	if err := os.Rename(fresh, target); err != nil {
		os.Rename(old, target) // ignore: best effort to undo; the error below says what failed
		return fmt.Errorf("put the new version in place: %w", err)
	}
	return nil
}
