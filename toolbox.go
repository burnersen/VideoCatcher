// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// toolbox brings the programs into place and tells the window how far it
// is: at start, and when the user clicks "Check for updates". Only one
// preparation runs at a time.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
)

// Tool states as the window shows them.
const (
	toolMissing     = "missing"
	toolChecking    = "checking"
	toolDownloading = "downloading"
	toolReady       = "ready"
	toolFailed      = "failed"
)

// ToolStatus is one line of the tools panel.
type ToolStatus struct {
	Tool     string `json:"tool"`     // yt-dlp, ffmpeg, deno
	Purpose  string `json:"purpose"`  // what it is needed for, in a few words
	Required bool   `json:"required"` // without it nothing can be downloaded
	State    string `json:"state"`
	Version  string `json:"version"`
	Done     int64  `json:"done"`  // bytes downloaded so far
	Total    int64  `json:"total"` // bytes expected, 0 if unknown
	Error    string `json:"error"` // message and hint, for the window
}

type toolbox struct {
	ts     toolSet
	client *http.Client
	logger *slog.Logger
	report func([]ToolStatus) // called on every change; may be nil
	rt     runtimeFile        // pinned FFmpeg and Deno (runtime.json)
	plat   string             // GOOS/GOARCH, a field so tests can pick one

	run    sync.Mutex // one preparation at a time
	mu     sync.Mutex // guards the fields below
	state  toolsState
	status map[string]*ToolStatus
}

var toolOrder = []string{"yt-dlp", "ffmpeg", "deno"}

func newToolbox(ts toolSet, client *http.Client, logger *slog.Logger, rt runtimeFile) *toolbox {
	tb := &toolbox{ts: ts, client: client, logger: logger, rt: rt, plat: thisPlatform(), status: map[string]*ToolStatus{
		"yt-dlp": {Tool: "yt-dlp", Purpose: "downloads the videos", Required: true, State: toolMissing},
		"ffmpeg": {Tool: "ffmpeg", Purpose: "joins picture and sound, subtitles, audio", State: toolMissing},
		"deno":   {Tool: "deno", Purpose: "needed by YouTube", State: toolMissing},
	}}
	tb.state = toolsState{Format: toolsStateFormat, Tools: map[string]toolEntry{}}
	return tb
}

// Statuses is a copy for the window, in a fixed order.
func (tb *toolbox) Statuses() []ToolStatus {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	out := make([]ToolStatus, 0, len(toolOrder))
	for _, t := range toolOrder {
		out = append(out, *tb.status[t])
	}
	return out
}

// Ready says whether a tool is installed and checked.
func (tb *toolbox) Ready(tool string) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.status[tool].State == toolReady
}

func (tb *toolbox) set(tool string, change func(s *ToolStatus)) {
	tb.mu.Lock()
	change(tb.status[tool])
	tb.mu.Unlock()
	if tb.report != nil {
		tb.report(tb.Statuses())
	}
}

// progressOf reports download progress of one tool, at most every 200 ms
// (progressReader takes care of that).
func (tb *toolbox) progressOf(tool string) func(done, total int64) {
	return func(done, total int64) {
		tb.set(tool, func(s *ToolStatus) { s.State, s.Done, s.Total = toolDownloading, done, total })
	}
}

// Prepare checks what is installed, downloads what is missing or damaged,
// and, if checkYtDlp is true, installs a newer yt-dlp. Errors are shown per
// tool; the returned error is the first one, for the log.
func (tb *toolbox) Prepare(ctx context.Context, checkYtDlp bool) error {
	tb.run.Lock()
	defer tb.run.Unlock()

	st, err := loadToolsState(tb.ts.State)
	if err != nil {
		return err // newer format: never overwrite it (05 K10)
	}
	tb.mu.Lock()
	tb.state = st
	tb.mu.Unlock()

	var errs []error
	errs = append(errs, tb.prepareYtDlp(ctx, &st, checkYtDlp))
	for _, tool := range []string{"ffmpeg", "deno"} {
		errs = append(errs, tb.preparePinned(ctx, &st, tool))
	}
	if err := saveToolsState(tb.ts.State, st); err != nil {
		tb.logger.Warn("cannot save the tool list", "err", err) // costs only a check next time
	}
	tb.mu.Lock()
	tb.state = st
	tb.mu.Unlock()
	return errors.Join(errs...)
}

func (tb *toolbox) prepareYtDlp(ctx context.Context, st *toolsState, check bool) error {
	cur := st.Tools["yt-dlp"]
	if !check && tb.ts.intact(cur) {
		tb.set("yt-dlp", func(s *ToolStatus) { s.State, s.Version, s.Error = toolReady, cur.Version, "" })
		return nil
	}
	tb.set("yt-dlp", func(s *ToolStatus) { s.State, s.Error = toolChecking, "" })
	res, err := updateYtDlp(ctx, tb.client, tb.logger, tb.ts, st, tb.progressOf("yt-dlp"))
	if err != nil {
		// A failed check is no problem while an installed, checked yt-dlp
		// is still there: it keeps working, the window says so.
		ok := tb.ts.intact(cur)
		tb.set("yt-dlp", func(s *ToolStatus) {
			s.Error = errorText(err)
			if ok {
				s.State, s.Version = toolReady, cur.Version
			} else {
				s.State = toolFailed
			}
		})
		return err
	}
	tb.set("yt-dlp", func(s *ToolStatus) { s.State, s.Version, s.Error = toolReady, res.Version, "" })
	return nil
}

func (tb *toolbox) preparePinned(ctx context.Context, st *toolsState, tool string) error {
	e, ok := tb.rt.runtimeFor(tool, tb.plat)
	if !ok {
		tb.set(tool, func(s *ToolStatus) { s.State, s.Error = toolMissing, "Not available for this computer." })
		return nil
	}
	cur := st.Tools[tool]
	if cur.Version == e.Name && tb.ts.intact(cur) {
		tb.set(tool, func(s *ToolStatus) { s.State, s.Version, s.Error = toolReady, e.Name, "" })
		return nil
	}
	tb.set(tool, func(s *ToolStatus) { s.State, s.Error = toolDownloading, "" })
	if err := installPinned(ctx, tb.client, tb.logger, tb.ts, st, e, tb.progressOf(tool)); err != nil {
		tb.set(tool, func(s *ToolStatus) { s.State, s.Error = toolFailed, errorText(err) })
		return err
	}
	tb.set(tool, func(s *ToolStatus) { s.State, s.Version, s.Error = toolReady, e.Name, "" })
	return nil
}

// errorText is the text of an error as the window shows it: message, then
// the hint on a second line. The technical cause stays in the log.
func errorText(err error) string {
	var ue *UserError
	if errors.As(err, &ue) {
		if ue.Hint == "" {
			return ue.Msg
		}
		return ue.Msg + "\n" + ue.Hint
	}
	if errors.Is(err, context.Canceled) {
		return "Stopped."
	}
	return "Something went wrong. Details are in the log file."
}
