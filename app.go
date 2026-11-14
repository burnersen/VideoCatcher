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
//     changed), "log" (a line for the activity log), "speed" (the speed
//     slider and what flows), "afterall" (when all downloads are done),
//     "update" (a newer VideoCatcher is out)

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
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
	speed  *speedControl // nil until startup
	after  *finishWatch
	tray   *trayIcon // nil if the system has none
	news   *trayNews // what the tray icon tells while the window is away
	awake  *awake    // keeps the PC from sleeping while downloads run
	client *http.Client

	quitting  atomic.Bool // Quit was chosen: the close button must not keep the window
	toldTray  atomic.Bool // the balloon "goes on here" was shown in this run
	saveTimer *time.Timer // settings the program changes itself, saved a moment later

	mu     sync.Mutex
	cfg    config
	probes map[string]MediaInfo // the last links looked up, for the preview
	order  []string             // probe IDs, oldest first
	logBuf []logLine            // the last lines of the activity log, for a page that loads later
	update string               // a newer VideoCatcher ("1.2.3"), or ""
	title  string               // the window title as it was last set
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
	a := &App{paths: paths, logger: logger, run: execRunner{}, probes: map[string]MediaInfo{}, ready: make(chan struct{}),
		news: newTrayNews(), awake: newAwake(), client: newHTTPClient(), title: displayName}
	a.tools = newToolbox(ts, a.client, logger, rt)
	a.queue = newQueue(a.run, ts, a.tools.Ready, paths.DataDir, logger)
	a.intake = newIntake(paths.DataDir, logger)
	a.cfg = defaultConfig()
	a.after = newFinishWatch(a.afterAllDone)
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	defer close(a.ready)
	a.queue.onChange = a.jobChanged
	a.after.onChange = a.afterAllChanged
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
	a.speed = newSpeedControl(a.cfg.SpeedPercent, float64(a.cfg.MeasuredSpeed), a.logger)
	a.queue.proxy = a.speed.ProxyURL
	go a.measureSpeed(ctx)
	if t, err := newTray(a.showWindow, a.quit); err != nil {
		a.logger.Warn("no tray icon, the close button quits", "err", err)
	} else {
		a.tray = t
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
	a.queue.Close() // running downloads keep their parts and continue at the next start
	a.awake.Set(false)
	if a.speed != nil {
		a.speed.Close()
	}
	if a.tray != nil {
		a.tray.Close()
	}
	a.mu.Lock()
	if a.saveTimer != nil && a.saveTimer.Stop() {
		a.mu.Unlock()
		a.saveNow() // a change from the last seconds must not get lost
	} else {
		a.mu.Unlock()
	}
	a.logger.Info("done")
}

// beforeClose decides what the close button does (setting closeButton):
// with downloads running, or always, the window goes into the notification
// area and the downloads go on. Quit from the tray menu really ends.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	a.rememberWindow(ctx)
	if a.quitting.Load() || a.tray == nil {
		return false
	}
	a.mu.Lock()
	mode := a.cfg.CloseAction
	a.mu.Unlock()
	if mode == "quit" || (mode == "tray-if-running" && a.queue.Active() == 0) {
		return false
	}
	if err := a.tray.Show(a.trayTip(), !a.toldTray.Load()); err != nil {
		a.logger.Warn("tray icon not shown, quitting instead", "err", err)
		return false
	}
	a.toldTray.Store(true)
	wailsruntime.WindowHide(ctx)
	a.logger.Info("window in the tray")
	return true
}

// rememberWindow keeps the size of the window for the next start. A
// maximised window keeps the size it had before, so "restore" gives that
// back; a minimised one reports no size worth keeping.
func (a *App) rememberWindow(ctx context.Context) {
	if wailsruntime.WindowIsMinimised(ctx) {
		return
	}
	a.mu.Lock()
	w, err := parseWindow(a.cfg.Window)
	a.mu.Unlock()
	if err != nil {
		return
	}
	if w.Maximised = wailsruntime.WindowIsMaximised(ctx); !w.Maximised {
		w.Width, w.Height = wailsruntime.WindowGetSize(ctx)
	}
	v := w.String()
	if _, err := parseWindow(v); err != nil {
		return // smaller than the window can be: not a size to come back to
	}
	a.mu.Lock()
	changed := a.cfg.Window != v
	a.cfg.Window = v
	a.mu.Unlock()
	if changed {
		a.saveSoon() // shutdown saves at once when the program ends now
	}
}

// showWindow brings the window back from the tray (or the taskbar).
func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	wailsruntime.WindowShow(a.ctx)
	wailsruntime.WindowUnminimise(a.ctx)
	if a.tray != nil {
		a.tray.Hide()
	}
}

// quit ends VideoCatcher, also when the close button would keep it.
func (a *App) quit() {
	a.quitting.Store(true)
	if a.ctx != nil {
		wailsruntime.Quit(a.ctx)
	}
}

func (a *App) trayTip() string {
	if a.tray == nil {
		return displayName
	}
	n := a.queue.Active()
	if n == 0 {
		return displayName
	}
	return strings.ReplaceAll(a.tray.Texts().Running, "{n}", fmt.Sprint(n))
}

// jobChanged: a download changed. The window gets it; the tray icon, the
// window title, "when all are done" and keeping the PC awake look at the
// numbers.
func (a *App) jobChanged(j Job) {
	a.emit("job", j)
	active := a.queue.Active()
	a.after.Update(active)
	a.awake.Set(active > 0)
	a.showTitle()
	if a.tray != nil && a.tray.Visible() {
		a.tray.SetTip(a.trayTip())
		if title, text := a.news.Update(j, active, a.tray.Texts()); text != "" {
			a.tray.Notify(title, text)
		}
	}
}

// windowTitle is the title of the window, which the taskbar shows too:
// how far the running downloads are, and how many there are.
func windowTitle(running int, percent float64) string {
	switch {
	case running <= 0:
		return displayName
	case running == 1:
		return fmt.Sprintf("%d %% · %s", int(percent), displayName)
	}
	return fmt.Sprintf("%d %% · %d downloads · %s", int(percent), running, displayName)
}

// showTitle sets the window title when it changed (progress comes several
// times a second, the title changes once per percent).
func (a *App) showTitle() {
	title := windowTitle(a.queue.Progress())
	a.mu.Lock()
	same := a.title == title
	a.title = title
	a.mu.Unlock()
	if !same && a.ctx != nil {
		wailsruntime.WindowSetTitle(a.ctx, title)
	}
}

// measureSpeed asks the relay once a second what flows and tells the
// window; a new fastest speed is saved (it is 100 % of the slider).
func (a *App) measureSpeed(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var last SpeedState
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		st, rose := a.speed.Tick()
		if rose {
			a.mu.Lock()
			a.cfg.MeasuredSpeed = int64(st.Peak)
			a.mu.Unlock()
			a.saveSoon()
		}
		if st != last {
			a.emit("speed", st)
			last = st
		}
	}
}

// saveSoon saves the settings the program changes itself (slider, measured
// speed) a few seconds after the last change, not at every step.
func (a *App) saveSoon() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.saveTimer != nil {
		a.saveTimer.Stop()
	}
	a.saveTimer = time.AfterFunc(3*time.Second, a.saveNow)
}

func (a *App) saveNow() {
	a.mu.Lock()
	c := a.cfg
	a.mu.Unlock()
	if err := saveConfig(a.paths.ConfigFile, c); err != nil {
		a.logger.Warn("settings not saved", "err", err)
	}
}

func (a *App) afterAllChanged(st AfterAllState) {
	a.emit("afterall", st)
	if st.Seconds > 0 {
		a.showWindow() // whoever is at the PC sees the countdown and can stop it
	}
}

// afterAllDone carries out the choice after the countdown.
func (a *App) afterAllDone(action string) {
	a.logger.Info("all downloads done", "action", action)
	var err error
	switch action {
	case "quit":
		a.quit()
	case "sleep":
		err = suspendPC(false)
	case "hibernate":
		err = suspendPC(true)
	case "shutdown":
		err = shutdownPC()
	}
	if err != nil {
		a.logError(userError(err, "Windows did not do it", "Do it yourself, or check whether this PC allows it (sleep and hibernate can be turned off)."))
	}
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
	if check {
		go a.lookForNewVersion()
	}
	if err := a.tools.Prepare(a.ctx, check); err != nil {
		a.logger.Warn("tools", "err", err)
		a.uiLogKey("warn", "log.notReady", "Not all programs are ready. The tools panel says why.", nil)
		return
	}
	a.uiLogKey("ok", "log.ready", "yt-dlp, FFmpeg and Deno are ready.", nil)
}

// lookForNewVersion asks whether a newer VideoCatcher was released and
// tells the window. No answer is no news: the look for yt-dlp reports a
// connection that is down.
func (a *App) lookForNewVersion() {
	ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
	defer cancel()
	v, err := newerApp(ctx, a.client, version)
	if err != nil {
		a.logger.Warn("look for a new version", "err", err)
		return
	}
	a.mu.Lock()
	known := a.update == v
	a.update = v
	a.mu.Unlock()
	if v == "" || known {
		return
	}
	a.logger.Info("a newer version is out", "version", v)
	a.emit("update", v)
	a.uiLogKey("info", "log.newVersion", displayName+" "+v+" is available. The link is at the bottom of the window.", map[string]string{"v": v})
}

// accept is where links from outside the window arrive (intake.go).
func (a *App) accept(req intakeRequest) {
	if req.Problem != nil {
		a.logError(req.Problem)
		return
	}
	a.mu.Lock()
	o := a.cfg.Defaults
	o.OutputDir = a.downloadDirLocked()
	a.mu.Unlock()
	if o.Playlist != "all" {
		// A stream from the extension has no title of its own for yt-dlp
		// ("index-v1-a1"); the page's title is the better name.
		o.FileName = safeFileName(req.FileName)
	}
	dropLogin := func() {
		for _, f := range append([]string{req.CookieFile, req.HeaderFile}, req.ListFiles...) {
			if f != "" {
				os.Remove(f) // ignore: a leftover is removed at the next start (removeOrphans)
			}
		}
	}
	// The same link again while it still waits or runs (a second click in
	// the browser): the one in the list is enough.
	if k, ok := a.queue.Known(req.URL); ok && !isFinished(k.Status) {
		dropLogin() // the job in the list has its own
		a.uiLogKey("info", "log.duplicate", "This link is already in the list: "+k.URL, map[string]string{"url": k.URL})
		return
	}
	j, err := a.queue.AddWith(req.URL, o, req.Source, req.Referer, req.Title, req.CookieFile,
		jobExtras{PageURL: req.PageURL, UserAgent: req.UserAgent, HeaderFile: req.HeaderFile, ListFiles: req.ListFiles})
	if err != nil {
		dropLogin() // the job that would have used it does not exist
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
	CloseButton  []string `json:"closeButton"`
	AfterAll     []string `json:"afterAll"`
}

// Settings are the app settings as the settings dialog edits them.
type Settings struct {
	Defaults       DownloadOptions `json:"defaults"`
	DownloadDir    string          `json:"downloadDir"` // "" = default
	Parallel       int             `json:"parallel"`
	CheckUpdates   bool            `json:"checkUpdates"`
	Language       string          `json:"language"`
	WatchClipboard bool            `json:"watchClipboard"`
	CloseButton    string          `json:"closeButton"`
}

// Initial is everything the page needs to draw itself.
type Initial struct {
	Version    string        `json:"version"`
	Settings   Settings      `json:"settings"`
	DefaultDir string        `json:"defaultDir"`
	Choices    Choices       `json:"choices"`
	Tools      []ToolStatus  `json:"tools"`
	Jobs       []Job         `json:"jobs"`
	Log        []logLine     `json:"log"`
	LogFile    string        `json:"logFile"`
	ConfigFile string        `json:"configFile"`
	Speed      SpeedState    `json:"speed"`
	AfterAll   AfterAllState `json:"afterAll"`
	HasTray    bool          `json:"hasTray"`
	Update     string        `json:"update"` // a newer VideoCatcher ("1.2.3"), or ""
}

func (a *App) Initial() Initial {
	<-a.ready
	a.mu.Lock()
	defer a.mu.Unlock()
	s := Settings{Defaults: a.cfg.Defaults, DownloadDir: a.cfg.DownloadDir, Parallel: a.cfg.Parallel,
		CheckUpdates: a.cfg.CheckUpdates, Language: a.cfg.Language, WatchClipboard: a.cfg.WatchClipboard,
		CloseButton: a.cfg.CloseAction}
	s.Defaults.OutputDir = a.downloadDirLocked()
	afterAll := afterAllChoices
	if goruntime.GOOS != "windows" {
		afterAll = []string{"none", "quit"} // power.go is Windows only for now
	}
	return Initial{Version: version, Settings: s, DefaultDir: defaultDownloadDir(), Tools: a.tools.Statuses(),
		Jobs: a.queue.List(), Log: append([]logLine{}, a.logBuf...), LogFile: a.paths.LogFile, ConfigFile: a.paths.ConfigFile,
		Choices: Choices{qualityChoices, containerChoices, audioFormatChoices, audioQualityChoices,
			subModeChoices, playlistChoices, sponsorChoices, cookieChoices, closeChoices, afterAll},
		Speed: a.speed.State(), AfterAll: a.after.State(), HasTray: a.tray != nil, Update: a.update}
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
	name = finalExtension(name, o)
	if o.CleanNames {
		name = tidyPath(name) // the name the file gets after the download (names.go)
	}
	return name, nil
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

// Known says whether a link is in the list already, and how: "" (no), or
// the state of its job (waiting, running, paused, confirm, done). The
// window asks before it adds a link a second time.
func (a *App) Known(link string) string {
	if j, ok := a.queue.Known(link); ok {
		return j.Status
	}
	return ""
}

func (a *App) StartNext(id string) error { return a.uiErr(a.queue.StartNext(id)) }
func (a *App) RetryFailed() int          { return a.queue.RetryFailed() }

func (a *App) Cancel(id string) error  { return a.uiErr(a.queue.Cancel(id)) }
func (a *App) Pause(id string) error   { return a.uiErr(a.queue.Pause(id)) }
func (a *App) Resume(id string) error  { return a.uiErr(a.queue.Resume(id)) }
func (a *App) PauseAll()               { a.queue.PauseAll() }
func (a *App) ResumeAll()              { a.queue.ResumeAll() }
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
	path, err := a.jobFile(id, index)
	if err != nil {
		return a.uiError(err)
	}
	if err := showInFolder(path); err != nil {
		return a.uiError(userError(err, "Cannot open Explorer", "Open the download folder yourself."))
	}
	return nil
}

// jobFile is a file a job itself reported (08: paths only from a known list).
func (a *App) jobFile(id string, index int) (string, error) {
	for _, j := range a.queue.List() {
		if j.ID == id && index >= 0 && index < len(j.Files) {
			return j.Files[index], nil
		}
	}
	return "", userError(fmt.Errorf("job %s file %d", id, index), "This file is not in the list", "")
}

// playable: the endings OpenFile hands to Windows. A download is opened
// with the program Windows has for it, and that must be a player: a site
// decides what a file is called, and a file that is a program or a script
// is only ever shown in its folder.
var playable = map[string]bool{
	".mp4": true, ".mkv": true, ".webm": true, ".mov": true, ".avi": true, ".flv": true, ".m4v": true,
	".ts": true, ".3gp": true, ".wmv": true, ".mpg": true, ".mpeg": true, ".ogv": true,
	".mp3": true, ".m4a": true, ".opus": true, ".ogg": true, ".oga": true, ".flac": true, ".wav": true,
	".aac": true, ".wma": true, ".mka": true, ".weba": true,
}

// OpenFile plays a downloaded file with the program Windows opens it with.
// A file that is no video or audio file is shown in its folder instead.
func (a *App) OpenFile(id string, index int) error {
	path, err := a.jobFile(id, index)
	if err != nil {
		return a.uiError(err)
	}
	if _, err := os.Stat(path); err != nil {
		return a.uiError(userError(err, "The file is no longer there", "It was moved, renamed or deleted after the download."))
	}
	if !playable[strings.ToLower(filepath.Ext(path))] {
		return a.ShowFile(id, index)
	}
	if err := openFile(path); err != nil {
		return a.uiError(userError(err, "Windows could not open the file", "Open it from the folder: \"Show in folder\"."))
	}
	return nil
}

// CopyLink puts the link of a job into the clipboard.
func (a *App) CopyLink(id string) error {
	return a.copyOf(id, func(j Job) string { return j.URL })
}

// CopyDetails puts everything a bug report needs about a job into the
// clipboard: what VideoCatcher said, and what yt-dlp said.
func (a *App) CopyDetails(id string) error {
	return a.copyOf(id, jobReport)
}

func (a *App) copyOf(id string, text func(Job) string) error {
	for _, j := range a.queue.List() {
		if j.ID != id {
			continue
		}
		if err := wailsruntime.ClipboardSetText(a.ctx, text(j)); err != nil {
			return a.uiError(userError(err, "The clipboard did not take the text", "Please try again."))
		}
		return nil
	}
	return a.uiError(userError(errors.New("unknown job "+id), "This download is no longer in the list", ""))
}

// jobReport is the text of "Copy details". The link is in it: whoever
// reports a problem needs it, and decides before pasting whether it may
// be seen. Logins are never part of a job.
func jobReport(j Job) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s (%s/%s)\n", displayName, version, goruntime.GOOS, goruntime.GOARCH)
	if j.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", j.Title)
	}
	fmt.Fprintf(&b, "Link: %s\nState: %s (%s)\n", j.URL, j.Status, j.Stage)
	if j.Error != "" {
		fmt.Fprintf(&b, "Message: %s\n", strings.ReplaceAll(j.Error, "\n", " "))
	}
	if len(j.Details) > 0 {
		b.WriteString("yt-dlp said:\n")
		for _, d := range j.Details {
			b.WriteString("  " + d + "\n")
		}
	}
	return b.String()
}

// OpenRelease opens the page of the newer version in the browser. The
// address is built here from the version that was found, never taken
// from the page.
func (a *App) OpenRelease() error {
	a.mu.Lock()
	v := a.update
	a.mu.Unlock()
	if v == "" {
		return a.uiError(userError(errors.New("no newer version known"), "There is no newer version", ""))
	}
	wailsruntime.BrowserOpenURL(a.ctx, releasePage(v))
	return nil
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
		CheckUpdates: s.CheckUpdates, Language: s.Language, WatchClipboard: s.WatchClipboard, CloseAction: s.CloseButton}
	c.Defaults.OutputDir = ""
	a.mu.Lock()
	// The slider and the measured speed are not in the dialog; they stay.
	c.SpeedPercent, c.MeasuredSpeed, c.Window = a.cfg.SpeedPercent, a.cfg.MeasuredSpeed, a.cfg.Window
	a.mu.Unlock()
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

// SetSpeed moves the speed slider (1 to 100 %). It counts at once, also for
// running downloads; the setting is saved a moment later.
func (a *App) SetSpeed(percent int) SpeedState {
	a.speed.SetPercent(percent)
	st := a.speed.State()
	a.mu.Lock()
	a.cfg.SpeedPercent = st.Percent
	a.mu.Unlock()
	a.saveSoon()
	return st
}

// SetAfterAll chooses what happens when all downloads are done.
func (a *App) SetAfterAll(action string) (AfterAllState, error) {
	if goruntime.GOOS != "windows" && action != "none" && action != "quit" {
		return a.after.State(), a.uiError(userError(errors.New("after all: "+action), "This is not possible on this system yet", ""))
	}
	if err := a.after.Set(action, a.queue.Active()); err != nil {
		return a.after.State(), a.uiError(err)
	}
	a.logger.Info("after all downloads", "action", action)
	return a.after.State(), nil
}

// AbortAfterAll stops the countdown and the choice.
func (a *App) AbortAfterAll() AfterAllState {
	a.after.Abort()
	return a.after.State()
}

// SetTrayTexts: the page tells the words for the tray, in its language.
func (a *App) SetTrayTexts(t TrayTexts) {
	if a.tray != nil {
		a.tray.SetTexts(t)
	}
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
