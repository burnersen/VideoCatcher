// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// Everything the page may call. Wails turns every exported method into a
// JavaScript function on window.go.main.App. Rules (template go-wails):
//
//   - one job per method; the page shows, Go decides
//   - every error goes through uiError: the page gets text a user can act
//     on, the log gets the technical cause
//   - nothing here blocks for long; long work runs in a goroutine and
//     reports with events: "tools" (the tools panel), "job" (one download
//     changed), "log" (a line for the activity log)

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the bridge between the page and the parts below.
type App struct {
	ctx    context.Context
	paths  appPaths
	logger *slog.Logger
	tools  *toolbox
	queue  *queue
	run    runner
	intake *intake

	mu     sync.Mutex
	cfg    config
	probes map[string]MediaInfo // the last links looked up, for the preview
	order  []string             // probe IDs, oldest first
	logBuf []logLine            // the last lines of the activity log, for a page that loads later
	ready  chan struct{}        // closed when startup is done
}

// maxProbes: looked-up links kept for the file name preview.
// maxLogBuf: activity lines kept for the page (it shows 300, the log file has all).
const (
	maxProbes = 20
	maxLogBuf = 100
)

// logLine is one line of the activity log. Key and Vars let the page show
// the line in its own language (i18n.js, "log.*"); Text is the English
// line for the log file and for a page that does not know the key.
type logLine struct {
	Level string            `json:"level"`
	Text  string            `json:"text"`
	Time  string            `json:"time"`
	Key   string            `json:"key,omitempty"`
	Vars  map[string]string `json:"vars,omitempty"`
}

func NewApp(paths appPaths, logger *slog.Logger) *App {
	ts := newToolSet(paths.DataDir)
	rt, err := loadRuntime()
	if err != nil {
		logger.Error("runtime.json", "err", err) // a build mistake; TestRuntimeJSONIsValid catches it
	}
	a := &App{paths: paths, logger: logger, run: execRunner{}, probes: map[string]MediaInfo{}, ready: make(chan struct{})}
	a.tools = newToolbox(ts, newHTTPClient(), logger, rt)
	a.queue = newQueue(a.run, ts, a.tools.Ready, paths.DataDir, logger)
	a.intake = newIntake(paths.DataDir, logger)
	a.cfg = defaultConfig()
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	defer close(a.ready)
	a.queue.onChange = func(j Job) { a.emit("job", j) }
	a.tools.report = func(s []ToolStatus) {
		a.emit("tools", s)
		a.queue.Schedule() // a tool that became ready may let waiting jobs start
	}

	res, err := loadConfig(a.paths.ConfigFile)
	if err != nil {
		a.logError(err)
	} else {
		a.cfg = res.Config
		if res.Created {
			a.uiLogKey("info", "log.configCreated", "Settings file created: "+a.paths.ConfigFile, map[string]string{"path": a.paths.ConfigFile})
		}
		for _, w := range res.Warnings {
			a.uiLog("warn", w)
		}
	}
	if err := a.queue.load(); err != nil {
		a.logError(err)
	}
	a.queue.SetParallel(a.cfg.Parallel)
	go a.prepareTools(a.cfg.CheckUpdates)
	go a.intake.watch(ctx, a.accept)
	if a.cfg.WatchClipboard {
		go a.watchClipboard(ctx)
	}
	a.refreshNativeHost()
	a.intake.handleArgs(os.Args[1:], a.accept)
}

// refreshNativeHost: the browser extension was turned on for a copy of
// VideoCatcher in another folder (an update downloaded somewhere else).
// The browsers then start this copy from now on.
func (a *App) refreshNativeHost() {
	exe, err := os.Executable()
	if err != nil || nativeHostState(a.paths.DataDir, exe) != "moved" {
		return
	}
	if err := registerNativeHost(a.paths.DataDir, exe); err != nil {
		a.logger.Warn("browser extension helper not updated", "err", err)
		return
	}
	a.logger.Info("browser extension helper now points here", "exe", exe)
}

func (a *App) shutdown(context.Context) {
	a.queue.Close()
	a.logger.Info("done")
}

// onSecondInstance: VideoCatcher was started again, for example with
// "--add <link>" or through a videocatcher:// link. The running window
// takes the job and comes to the front.
func (a *App) onSecondInstance(data options.SecondInstanceData) {
	<-a.ready
	a.intake.handleArgs(data.Args, a.accept)
	wailsruntime.WindowUnminimise(a.ctx)
	wailsruntime.WindowShow(a.ctx)
}

func (a *App) emit(name string, data any) {
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, name, data)
	}
}

// uiLog writes a line into the activity log of the window. level: info,
// ok, warn, error.
func (a *App) uiLog(level, text string) { a.uiLogKey(level, "", text, nil) }

// uiLogKey adds a line the page can translate: key names the text in
// i18n.js, vars fill its {placeholders}.
func (a *App) uiLogKey(level, key, text string, vars map[string]string) {
	l := logLine{Level: level, Text: text, Time: time.Now().Format("15:04:05"), Key: key, Vars: vars}
	a.mu.Lock()
	a.logBuf = append(a.logBuf, l)
	if len(a.logBuf) > maxLogBuf {
		a.logBuf = a.logBuf[len(a.logBuf)-maxLogBuf:]
	}
	a.mu.Unlock()
	a.emit("log", l)
}

// logError shows an error from a background task in the activity log.
func (a *App) logError(err error) {
	a.uiLog("error", strings.ReplaceAll(a.uiError(err).Error(), "\n", " "))
}

func (a *App) prepareTools(check bool) {
	if err := a.tools.Prepare(a.ctx, check); err != nil {
		a.logger.Warn("tools", "err", err)
		a.uiLogKey("warn", "log.notReady", "Not all programs are ready. The tools panel says why.", nil)
		return
	}
	a.uiLogKey("ok", "log.ready", "yt-dlp, FFmpeg and Deno are ready.", nil)
}

// accept is where links from outside the window arrive (intake.go).
func (a *App) accept(req intakeRequest) {
	a.mu.Lock()
	o := a.cfg.Defaults
	o.OutputDir = a.downloadDirLocked()
	a.mu.Unlock()
	if o.Playlist != "all" {
		// A stream from the extension has no title of its own for yt-dlp
		// ("index-v1-a1"); the page's title is the better name.
		o.FileName = safeFileName(req.FileName)
	}
	j, err := a.queue.Add(req.URL, o, req.Source, req.Referer, req.Title, req.CookieFile)
	if err != nil {
		if req.CookieFile != "" {
			os.Remove(req.CookieFile) // ignore: the job that would have used it does not exist
		}
		a.logError(err)
		return
	}
	if j.Status == jobConfirm {
		a.uiLogKey("warn", "log.webLink", "A web page sent a link. It waits in the list until you click OK: "+j.URL, map[string]string{"url": j.URL})
	} else {
		a.uiLogKey("info", "log.received", "Link received: "+j.URL, map[string]string{"url": j.URL})
	}
}

// --- what the page calls ---

// Version is shown in the footer.
func (a *App) Version() string { return version }

// Choices are the lists the menus are built from (same lists as the checks).
type Choices struct {
	Quality      []string `json:"quality"`
	Container    []string `json:"container"`
	AudioFormat  []string `json:"audioFormat"`
	AudioQuality []string `json:"audioQuality"`
	SubMode      []string `json:"subMode"`
	Playlist     []string `json:"playlist"`
	SponsorBlock []string `json:"sponsorBlock"`
	Cookies      []string `json:"cookies"`
}

// Settings are the app settings as the settings dialog edits them.
type Settings struct {
	Defaults       DownloadOptions `json:"defaults"`
	DownloadDir    string          `json:"downloadDir"` // "" = default
	Parallel       int             `json:"parallel"`
	CheckUpdates   bool            `json:"checkUpdates"`
	Language       string          `json:"language"`
	WatchClipboard bool            `json:"watchClipboard"`
}

// Initial is everything the page needs to draw itself.
type Initial struct {
	Version    string       `json:"version"`
	Settings   Settings     `json:"settings"`
	DefaultDir string       `json:"defaultDir"`
	Choices    Choices      `json:"choices"`
	Tools      []ToolStatus `json:"tools"`
	Jobs       []Job        `json:"jobs"`
	Log        []logLine    `json:"log"`
	LogFile    string       `json:"logFile"`
	ConfigFile string       `json:"configFile"`
}

func (a *App) Initial() Initial {
	<-a.ready
	a.mu.Lock()
	defer a.mu.Unlock()
	s := Settings{Defaults: a.cfg.Defaults, DownloadDir: a.cfg.DownloadDir, Parallel: a.cfg.Parallel,
		CheckUpdates: a.cfg.CheckUpdates, Language: a.cfg.Language, WatchClipboard: a.cfg.WatchClipboard}
	s.Defaults.OutputDir = a.downloadDirLocked()
	return Initial{Version: version, Settings: s, DefaultDir: defaultDownloadDir(), Tools: a.tools.Statuses(),
		Jobs: a.queue.List(), Log: append([]logLine{}, a.logBuf...), LogFile: a.paths.LogFile, ConfigFile: a.paths.ConfigFile,
		Choices: Choices{qualityChoices, containerChoices, audioFormatChoices, audioQualityChoices,
			subModeChoices, playlistChoices, sponsorChoices, cookieChoices}}
}

// downloadDirLocked is the folder downloads go to; a.mu is held.
func (a *App) downloadDirLocked() string {
	if a.cfg.DownloadDir != "" {
		return a.cfg.DownloadDir
	}
	return defaultDownloadDir()
}

// defaultDownloadDir is Downloads\VideoCatcher in the user's folder.
func defaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Downloads", appName)
}

// Probe asks yt-dlp what is behind a link.
func (a *App) Probe(link, playlist, cookies string) (MediaInfo, error) {
	u, err := checkURL(link)
	if err != nil {
		return MediaInfo{}, a.uiError(err)
	}
	if !a.tools.Ready("yt-dlp") {
		return MediaInfo{}, a.uiError(userError(errors.New("yt-dlp not ready"),
			"yt-dlp is not ready yet", "Wait until the tools panel shows it as ready."))
	}
	ps := probeSettings{Playlist: playlist, Cookies: cookies}
	if oneOf("playlist", playlist, playlistChoices) != nil || oneOf("cookies", cookies, cookieChoices) != nil {
		return MediaInfo{}, a.uiError(userError(fmt.Errorf("probe %q %q", playlist, cookies), "A setting is not valid", "Choose a value from the list."))
	}
	ctx, cancel := context.WithTimeout(a.ctx, 3*time.Minute)
	defer cancel()
	info, err := probe(ctx, a.run, a.tools.ts.YtDlp, probeArgs(u, ps, a.tools.ts, a.tools.Ready, ""))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			err = userError(err, "The site took too long to answer", "Try again later.")
		}
		return MediaInfo{}, a.uiError(err)
	}
	if info.URL == "" {
		info.URL = u
	}
	info.Options = ps
	a.mu.Lock()
	info.ProbeID = fmt.Sprintf("p%d", time.Now().UnixNano())
	a.probes[info.ProbeID] = info
	a.order = append(a.order, info.ProbeID)
	if len(a.order) > maxProbes {
		delete(a.probes, a.order[0])
		a.order = a.order[1:]
	}
	a.mu.Unlock()
	a.logger.Info("probed", "host", hostOf(u), "playlist", info.IsPlaylist, "formats", len(info.Formats))
	return info, nil
}

// Preview gives the full path a download would get. For a playlist it is
// only the template with the folder: the entries are not known in detail.
func (a *App) Preview(probeID string, o DownloadOptions) (string, error) {
	if err := o.validate(); err != nil {
		return "", a.uiError(err)
	}
	a.mu.Lock()
	info, ok := a.probes[probeID]
	a.mu.Unlock()
	if !ok || info.IsPlaylist {
		return filepath.Join(o.OutputDir, o.outputTemplate()), nil
	}
	dir := filepath.Join(a.paths.DataDir, "partial")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", a.uiError(fileError(err, "create the folder", dir))
	}
	f, err := os.CreateTemp(dir, "preview-*.info.json")
	if err != nil {
		return "", a.uiError(fileError(err, "create a file in", dir))
	}
	defer os.Remove(f.Name()) // ignore: a temporary file, gone either way soon
	_, werr := f.Write(info.Raw)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return "", a.uiError(fileError(werr, "write", f.Name()))
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	var name string
	var errs []string
	err = a.run.Run(ctx, a.tools.ts.YtDlp, previewArgs(f.Name(), o), func(isErr bool, line string) {
		if l := parseLine(line); l.Kind == lineError {
			errs = append(errs, l.Text)
		} else if !isErr && line != "" && l.Kind == lineOther {
			name = line
		}
	})
	if err != nil || name == "" {
		return "", a.uiError(explainYtDlp(errs, err))
	}
	return finalExtension(name, o), nil
}

// Add puts a download into the list.
func (a *App) Add(link string, o DownloadOptions, title string) (Job, error) {
	j, err := a.queue.Add(link, o, "window", "", title, "")
	if err != nil {
		return Job{}, a.uiError(err)
	}
	if !a.tools.Ready("yt-dlp") {
		a.uiLogKey("info", "log.waitTools", "The download starts as soon as yt-dlp is ready.", nil)
	}
	return j, nil
}

// Clipboard returns the clipboard text if it is a web link, else "".
func (a *App) Clipboard() string {
	text, err := wailsruntime.ClipboardGetText(a.ctx)
	if err != nil {
		return ""
	}
	if u, err := checkURL(text); err == nil {
		return u
	}
	return ""
}

func (a *App) Cancel(id string) error  { return a.uiErr(a.queue.Cancel(id)) }
func (a *App) Retry(id string) error   { return a.uiErr(a.queue.Retry(id)) }
func (a *App) Confirm(id string) error { return a.uiErr(a.queue.Confirm(id)) }
func (a *App) Remove(id string)        { a.queue.Remove(id) }
func (a *App) ClearFinished() []Job    { a.queue.Remove(a.queue.FinishedIDs()...); return a.queue.List() }
func (a *App) Jobs() []Job             { return a.queue.List() }

func (a *App) uiErr(err error) error {
	if err == nil {
		return nil
	}
	return a.uiError(err)
}

// ShowFile opens Explorer at the downloaded file of a job. Only files the
// job itself reported can be opened (08: paths only from a known list).
func (a *App) ShowFile(id string, index int) error {
	for _, j := range a.queue.List() {
		if j.ID != id {
			continue
		}
		if index < 0 || index >= len(j.Files) {
			break
		}
		if err := showInFolder(j.Files[index]); err != nil {
			return a.uiError(userError(err, "Cannot open Explorer", "Open the download folder yourself."))
		}
		return nil
	}
	return a.uiError(userError(fmt.Errorf("job %s file %d", id, index), "This file is not in the list", ""))
}

// OpenFolder opens the download folder, or the folder of the log file.
func (a *App) OpenFolder(which string) error {
	a.mu.Lock()
	dir := a.downloadDirLocked()
	a.mu.Unlock()
	if which == "log" {
		dir = filepath.Dir(a.paths.LogFile)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return a.uiError(fileError(err, "create the folder", dir))
	}
	if err := openFolder(dir); err != nil {
		return a.uiError(userError(err, "Cannot open Explorer", dir))
	}
	return nil
}

// ChooseFolder opens the folder dialog; "" means cancelled.
func (a *App) ChooseFolder(current string) (string, error) {
	dir, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Choose the download folder", DefaultDirectory: current, CanCreateDirectories: true})
	if err != nil {
		return "", a.uiError(userError(err, "The folder dialog did not open", "Please try again."))
	}
	return dir, nil
}

// SaveSettings stores the settings and uses them from now on.
func (a *App) SaveSettings(s Settings) error {
	c := config{Defaults: s.Defaults, DownloadDir: s.DownloadDir, Parallel: s.Parallel,
		CheckUpdates: s.CheckUpdates, Language: s.Language, WatchClipboard: s.WatchClipboard}
	c.Defaults.OutputDir = ""
	if err := saveConfig(a.paths.ConfigFile, c); err != nil {
		return a.uiError(err)
	}
	a.mu.Lock()
	watchBefore := a.cfg.WatchClipboard
	a.cfg = c
	a.mu.Unlock()
	a.queue.SetParallel(c.Parallel)
	if c.WatchClipboard && !watchBefore {
		go a.watchClipboard(a.ctx)
	}
	a.logger.Info("settings saved")
	return nil
}

// CheckUpdates looks for a new yt-dlp now. It refuses while downloads run:
// yt-dlp is replaced, and a running one must not lose its program.
func (a *App) CheckUpdates() error {
	if a.queue.Hold() {
		a.queue.Release()
		return a.uiError(userError(errors.New("downloads running"),
			"Downloads are running right now", "Check for updates when they are done."))
	}
	go func() {
		defer a.queue.Release()
		a.prepareTools(true)
	}()
	return nil
}

// watchClipboard offers links that are copied while VideoCatcher runs. The
// page decides what to do with them (it shows them, nothing starts).
func (a *App) watchClipboard(ctx context.Context) {
	last := a.Clipboard()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		a.mu.Lock()
		on := a.cfg.WatchClipboard
		a.mu.Unlock()
		if !on {
			return
		}
		if u := a.Clipboard(); u != "" && u != last {
			last = u
			a.emit("clipboard", u)
		}
	}
}

// BrowserLinks says whether videocatcher:// links open VideoCatcher.
func (a *App) BrowserLinks() bool { return protocolRegistered() }

// BrowserExtension says whether the browsers know VideoCatcher's helper
// for the extension (nativehost.go).
func (a *App) BrowserExtension() bool {
	exe, err := os.Executable()
	return err == nil && nativeHostState(a.paths.DataDir, exe) == "on"
}

// SetBrowserExtension tells Firefox, Chrome and Edge where the helper is,
// or makes them forget it.
func (a *App) SetBrowserExtension(on bool) error {
	exe, err := os.Executable()
	if err == nil {
		if on {
			err = registerNativeHost(a.paths.DataDir, exe)
		} else {
			err = unregisterNativeHost()
		}
	}
	if err != nil {
		return a.uiError(userError(err, "The browser extension could not be connected", "Try again. The log file has the details."))
	}
	a.logger.Info("browser extension helper", "on", on)
	return nil
}

// SetBrowserLinks turns videocatcher:// links on or off for this user.
func (a *App) SetBrowserLinks(on bool) error {
	var err error
	if on {
		err = registerProtocol()
	} else {
		err = unregisterProtocol()
	}
	if err != nil {
		return a.uiError(userError(err, "Browser links could not be changed", "Try again. The log file has the details."))
	}
	a.logger.Info("browser links", "on", on)
	return nil
}
