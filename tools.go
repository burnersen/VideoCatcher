// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// The programs VideoCatcher runs: yt-dlp (does the downloading), FFmpeg
// (joins picture and sound, embeds subtitles, converts audio) and Deno
// (runs the JavaScript that YouTube requires since yt-dlp 2025.11.12).
// None of them is shipped; all are downloaded into one folder on first need.
//
// FFmpeg and Deno are pinned in runtime.json by size and SHA-256 (08 S4). A
// newer build: change runtime.json, run the workflow "Mirror runtime", then
// release. yt-dlp is the exception: it changes every few weeks because the
// sites change, so the newest release is used, but only if its checksum list
// is signed by the yt-dlp release key (ytdlp.go).
//
// tools.json remembers the SHA-256 of every installed program. At start the
// files are checked against it; a file that changed is downloaded again.

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

//go:embed runtime.json
var runtimeJSON []byte

// runtimeEntry is one pinned download as runtime.json describes it.
type runtimeEntry struct {
	Tool     string   `json:"tool"`     // "ffmpeg" or "deno"
	Platform string   `json:"platform"` // GOOS/GOARCH
	Name     string   `json:"name"`
	File     string   `json:"file"`
	Size     int64    `json:"size"`
	SHA256   string   `json:"sha256"`
	License  string   `json:"license"`
	Build    string   `json:"build"`
	Source   string   `json:"source"`
	Extract  []string `json:"extract"` // paths inside the archive, below its root folder
	URLs     []string `json:"urls"`
}

type runtimeFile struct {
	Format int            `json:"format"`
	Tools  []runtimeEntry `json:"tools"`
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// loadRuntime reads the embedded runtime.json. A broken file is a build
// mistake, so the tests check it too.
func loadRuntime() (runtimeFile, error) {
	var rt runtimeFile
	if err := json.Unmarshal(runtimeJSON, &rt); err != nil {
		return rt, fmt.Errorf("read runtime.json: %w", err)
	}
	if rt.Format != 1 {
		return rt, fmt.Errorf("runtime.json: format %d, expected 1", rt.Format)
	}
	for _, e := range rt.Tools {
		switch {
		case e.Tool != "ffmpeg" && e.Tool != "deno":
			return rt, fmt.Errorf("runtime.json: unknown tool %q", e.Tool)
		case e.Size <= 0 || !sha256Hex.MatchString(e.SHA256) || len(e.URLs) == 0 || len(e.Extract) == 0:
			return rt, fmt.Errorf("runtime.json: %s %s needs size, sha256 (lower case hex), extract and urls", e.Tool, e.Platform)
		case e.License == "" || e.Source == "":
			return rt, fmt.Errorf("runtime.json: %s %s needs license and source", e.Tool, e.Platform)
		}
		for _, u := range e.URLs {
			if !strings.HasPrefix(u, "https://") || !strings.HasSuffix(u, "/"+e.File) {
				return rt, fmt.Errorf("runtime.json: url %s must be https and end with %s", u, e.File)
			}
		}
		for _, x := range e.Extract {
			if x != path.Clean(x) || strings.HasPrefix(x, "/") || strings.Contains(x, "..") {
				return rt, fmt.Errorf("runtime.json: extract path %q is not a plain relative path", x)
			}
		}
	}
	return rt, nil
}

// runtimeFor returns the pinned entry of tool for this computer.
func (rt runtimeFile) runtimeFor(tool, platform string) (runtimeEntry, bool) {
	for _, e := range rt.Tools {
		if e.Tool == tool && e.Platform == platform {
			return e, true
		}
	}
	return runtimeEntry{}, false
}

func thisPlatform() string { return runtime.GOOS + "/" + runtime.GOARCH }

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// toolSet is where the programs are once installed.
type toolSet struct {
	Dir     string
	YtDlp   string
	FFmpeg  string
	FFprobe string
	Deno    string
	State   string // tools.json
}

func newToolSet(dataDir string) toolSet {
	dir := filepath.Join(dataDir, "bin")
	return toolSet{
		Dir:     dir,
		YtDlp:   filepath.Join(dir, exeName("yt-dlp")),
		FFmpeg:  filepath.Join(dir, exeName("ffmpeg")),
		FFprobe: filepath.Join(dir, exeName("ffprobe")),
		Deno:    filepath.Join(dir, exeName("deno")),
		State:   filepath.Join(dataDir, "tools.json"),
	}
}

// toolsStateFormat is the version of tools.json (05 K10).
const toolsStateFormat = 1

// toolsState is what tools.json holds: per installed program its version
// and the SHA-256 of every file that belongs to it.
type toolsState struct {
	Format int                  `json:"format"`
	Tools  map[string]toolEntry `json:"tools"`
}

type toolEntry struct {
	Version string            `json:"version"`
	Files   map[string]string `json:"files"` // file name in the bin folder -> SHA-256
}

func loadToolsState(path string) (toolsState, error) {
	st := toolsState{Format: toolsStateFormat, Tools: map[string]toolEntry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, fileError(err, "read", path)
	}
	var probe struct {
		Format int `json:"format"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		// Broken: forget it. The worst case is one more download.
		return st, nil
	}
	if probe.Format > toolsStateFormat {
		return st, userError(fmt.Errorf("%s has format %d", path, probe.Format),
			"The list of installed programs was made by a newer version of VideoCatcher",
			"Please update VideoCatcher.")
	}
	if err := json.Unmarshal(data, &st); err != nil || st.Tools == nil {
		return toolsState{Format: toolsStateFormat, Tools: map[string]toolEntry{}}, nil
	}
	return st, nil
}

func saveToolsState(path string, st toolsState) error {
	st.Format = toolsStateFormat
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode tools state: %w", err)
	}
	return writeFileAtomic(path, append(data, '\n'), 0o644)
}

// intact says whether every file of an installed tool is still the one that
// was checked when it was installed.
func (ts toolSet) intact(e toolEntry) bool {
	if len(e.Files) == 0 {
		return false
	}
	for name, want := range e.Files {
		got, err := fileSHA256(filepath.Join(ts.Dir, name))
		if err != nil || got != want {
			return false
		}
	}
	return true
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close() // read only: a close error changes nothing
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractFiles unpacks exactly the wanted files from a ZIP archive into dir
// and returns their SHA-256 by file name. An archive usually has one root
// folder ("ffmpeg-n9.0.2-.../bin/ffmpeg.exe"); a wanted path matches with or
// without it. Each file goes through ".part", so an interrupted unpack never
// leaves half a program behind.
func extractFiles(zipPath, dir string, wanted []string) (map[string]string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fileError(err, "open the archive", zipPath)
	}
	defer zr.Close()

	found := map[string]string{}
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		for _, w := range wanted {
			_, belowRoot, _ := strings.Cut(name, "/")
			if name != w && belowRoot != w {
				continue
			}
			base := path.Base(w)
			sum, err := extractZipEntry(f, filepath.Join(dir, base))
			if err != nil {
				return nil, err
			}
			found[base] = sum
		}
	}
	if len(found) != len(wanted) {
		return nil, fmt.Errorf("only %d of %d programs found in %s", len(found), len(wanted), filepath.Base(zipPath))
	}
	return found, nil
}

func extractZipEntry(f *zip.File, dest string) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", fmt.Errorf("open %s in the archive: %w", f.Name, err)
	}
	defer rc.Close()

	part := dest + ".part"
	out, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", fileError(err, "create", part)
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), rc)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(part, dest)
	}
	if err != nil {
		os.Remove(part) // ignore: cleanup after the error that is returned
		return "", fileError(err, "unpack", dest)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// installPinned downloads the pinned archive of e, unpacks its programs into
// the bin folder and records them in the state. The archive is removed
// afterwards; only the programs are kept.
func installPinned(ctx context.Context, client *http.Client, logger *slog.Logger, ts toolSet, st *toolsState, e runtimeEntry, show func(done, total int64)) error {
	if err := os.MkdirAll(ts.Dir, 0o755); err != nil {
		return fileError(err, "create the folder", ts.Dir)
	}
	zipPath := filepath.Join(ts.Dir, e.File)
	defer os.Remove(zipPath) // ignore: only the programs are kept
	f := pinnedFile{Name: e.Name, Size: e.Size, SHA256: e.SHA256}
	if err := fetchPinned(ctx, client, logger, f, e.URLs, zipPath, show); err != nil {
		return err
	}
	files, err := extractFiles(zipPath, ts.Dir, e.Extract)
	if err != nil {
		return userError(err, "Cannot unpack "+e.Name, "Try again. If it keeps failing, the log file has the details.")
	}
	st.Tools[e.Tool] = toolEntry{Version: e.Name, Files: files}
	logger.Info("installed", "tool", e.Tool, "name", e.Name)
	return nil
}
