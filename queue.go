// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// The download manager: a list of jobs, of which at most "parallel" run at
// the same time. Each job is one run of yt-dlp. The list is saved in
// queue.json after every change of state, so it survives a restart.
//
// Unfinished parts live in a folder of the job below the program's data
// folder, never in the download folder (README promise: no half files in
// your folder). The folder stays while a job is paused, and when
// VideoCatcher ends during a download: the next run of yt-dlp continues
// the parts where they stopped. It goes when the job is done, failed,
// stopped or removed.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Job states.
const (
	jobConfirm   = "confirm" // came from outside (a web page); waits for the user's OK
	jobWaiting   = "waiting"
	jobRunning   = "running"
	jobPaused    = "paused" // stopped by the user, parts kept; waits for "Continue"
	jobDone      = "done"
	jobFailed    = "failed"
	jobCancelled = "cancelled"
)

// Job is one download, as the window shows it and queue.json keeps it.
type Job struct {
	ID        string          `json:"id"`
	URL       string          `json:"url"`
	Title     string          `json:"title"`
	Source    string          `json:"source"` // window, clipboard, cli, protocol, extension
	Referer   string          `json:"referer,omitempty"`
	Options   DownloadOptions `json:"options"`
	Status    string          `json:"status"`
	Stage     string          `json:"stage"` // what happens right now, in words
	Percent   float64         `json:"percent"`
	Done      int64           `json:"done"`
	Total     int64           `json:"total"`
	Estimated bool            `json:"estimated"`
	Speed     float64         `json:"speed"`
	ETA       int64           `json:"eta"`
	Item      int64           `json:"item"`  // position in a playlist, 0 if none
	Items     int64           `json:"items"` // entries of the playlist
	ItemTitle string          `json:"itemTitle"`
	Files     []string        `json:"files"`
	Error     string          `json:"error"`   // message and hint
	Details   []string        `json:"details"` // yt-dlp's own error and warning lines
	Added     time.Time       `json:"added"`
	Started   time.Time       `json:"started,omitempty"`
	Ended     time.Time       `json:"ended,omitempty"`
	Resumable bool            `json:"resumable,omitempty"` // parts of an earlier run are kept; yt-dlp continues them
	Size      int64           `json:"size,omitempty"`      // bytes of the downloaded files, when the job is done
	Elapsed   float64         `json:"elapsed,omitempty"`   // seconds the job ran, all runs together
	Skipped   int             `json:"skipped,omitempty"`   // a playlist that is done: entries yt-dlp could not download
	Next      bool            `json:"next,omitempty"`      // waiting, and the user wants it before the other waiting ones
	Tries     int             `json:"tries,omitempty"`     // runs that ended with trouble on the way to the site
	RetryAt   time.Time       `json:"retryAt,omitzero"`    // waiting, but not before this time (a new try by itself)
	// CookieName is the name of the job's cookie file in the cookie folder,
	// so a paused job still has its login after a restart. Only the name:
	// the cookies themselves never go into queue.json.
	CookieName string `json:"cookieName,omitempty"`
	// HeaderName is the same for the file with the request headers of the
	// browser (headers.go).
	HeaderName string `json:"headerName,omitempty"`
	// ListNames are the files of a list the player built in memory
	// (memlist.go), in the same folder; the first is the one yt-dlp gets
	// instead of the link.
	ListNames []string `json:"listNames,omitempty"`
	// PageURL is the page a video link was found on, UserAgent the browser
	// that found it (both from the extension). A link with a token in it
	// is only good for a while: when the site refuses it, the job tries
	// the page instead, once (ViaPage).
	PageURL   string `json:"pageUrl,omitempty"`
	UserAgent string `json:"userAgent,omitempty"`
	ViaPage   bool   `json:"viaPage,omitempty"`
	// AsBrowser: the site turned yt-dlp away where it lets a browser in (a
	// bot wall like Cloudflare's, or a plain 403). The job then runs once
	// more with yt-dlp answering like a browser (--impersonate). NoBrowser:
	// this yt-dlp cannot do that, so it is not asked again.
	AsBrowser bool `json:"asBrowser,omitempty"`
	NoBrowser bool `json:"noBrowser,omitempty"`

	follow string   // set for a try the job starts by itself: what it tries; the details of the tries before stay
	before []string // yt-dlp's error lines of those tries, for the message at the end

	cookieFile string // from the browser extension; deleted when the job ends
	headerFile string // the same, for the request headers
	listFiles  []string
}

// jobExtras is what the browser extension knows beyond the link.
type jobExtras struct {
	PageURL    string // the page the link was found on
	UserAgent  string // the browser's own name for itself
	HeaderFile string // request headers for yt-dlp, written by intake.go
	ListFiles  []string
}

// Limits (05 K9).
const (
	maxDetails     = 40                     // yt-dlp lines kept per job; the log has all
	maxHistory     = 500                    // finished jobs kept in the list
	updateInterval = 250 * time.Millisecond // progress to the window, per job
	queueFormat    = 1
)

// retryDelays: after trouble on the way to the site (the connection
// dropped), a download waits this long and tries again, once per entry.
// yt-dlp has tried ten times itself by then, so the trouble lasts; more
// tries than these only keep a dead link in the list for longer.
var retryDelays = []time.Duration{30 * time.Second, 2 * time.Minute}

type queue struct {
	r         runner
	ts        toolSet
	have      func(tool string) bool
	tempRoot  string // parts of unfinished downloads
	cookieDir string // cookie files from the extension (intake.go writes them)
	file      string // queue.json
	logger    *slog.Logger
	onChange  func(Job)     // the window; may be nil
	proxy     func() string // the relay for the speed limit (speed.go); may be nil

	mu       sync.Mutex
	jobs     []*Job
	cancel   map[string]context.CancelFunc
	parallel int
	hold     bool            // no new starts, e.g. while yt-dlp is being replaced
	delays   []time.Duration // retryDelays; a field so tests need not wait
	counter  int
	lastSent map[string]time.Time
	wg       sync.WaitGroup
	closing  bool            // stopping because VideoCatcher ends: running jobs wait for the next start
	pausing  map[string]bool // running jobs the user paused; they end as paused, not stopped
	closed   bool
}

func newQueue(r runner, ts toolSet, have func(string) bool, dataDir string, logger *slog.Logger) *queue {
	return &queue{r: r, ts: ts, have: have, logger: logger, parallel: 2, delays: retryDelays,
		tempRoot: filepath.Join(dataDir, "partial"), cookieDir: filepath.Join(dataDir, "cookies"),
		file: filepath.Join(dataDir, "queue.json"), cancel: map[string]context.CancelFunc{},
		lastSent: map[string]time.Time{}, pausing: map[string]bool{}}
}

type queueFile struct {
	Format int    `json:"format"`
	Jobs   []*Job `json:"jobs"`
}

// load reads queue.json. Running jobs of the last session wait again and
// continue their parts. Part folders and cookie files no job needs any
// more (a crash, an old version) are removed.
func (q *queue) load() error {
	defer q.removeOrphans()
	data, err := os.ReadFile(q.file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fileError(err, "read the download list", q.file)
	}
	var f queueFile
	if err := json.Unmarshal(data, &f); err != nil {
		// Keep the broken file for a look, start with an empty list (05 K10).
		os.Rename(q.file, q.file+".broken") // ignore: if it fails, the next save replaces it
		q.logger.Warn("download list unreadable, starting empty", "file", q.file, "err", err)
		return nil
	}
	if f.Format > queueFormat {
		return userError(fmt.Errorf("%s has format %d", q.file, f.Format),
			"The download list was made by a newer version of VideoCatcher",
			"Please update VideoCatcher. The list was not changed.")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, j := range f.Jobs {
		if j == nil || j.ID == "" {
			continue
		}
		if j.Status == jobRunning {
			j.Status, j.Stage, j.Resumable = jobWaiting, "interrupted, continues", true
		}
		j.RetryAt = time.Time{} // the wait belonged to the last session; a new start tries at once

		if !isFinished(j.Status) {
			j.cookieFile, j.headerFile = q.loginFile(j.CookieName), q.loginFile(j.HeaderName)
			for _, n := range j.ListNames {
				j.listFiles = append(j.listFiles, q.loginFile(n))
			}
		}
		q.jobs = append(q.jobs, j)
	}
	return nil
}

// loginFile is the path of a cookie or header file by the name a job
// keeps, or "" if there is none any more.
func (q *queue) loginFile(name string) string {
	if p := filepath.Join(q.cookieDir, name); name != "" && filepath.Base(name) == name && fileExists(p) {
		return p
	}
	return ""
}

// saveLocked writes queue.json; q.mu is held.
func (q *queue) saveLocked() {
	if q.closed {
		return
	}
	data, err := json.MarshalIndent(queueFile{Format: queueFormat, Jobs: q.jobs}, "", " ")
	if err == nil {
		err = writeFileAtomic(q.file, data, 0o600) // links can be private: only for this user
	}
	if err != nil {
		// The downloads go on; only the list would not survive a restart.
		q.logger.Warn("cannot save the download list", "err", err)
	}
}

// List is a copy of all jobs, oldest first.
func (q *queue) List() []Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Job, 0, len(q.jobs))
	for _, j := range q.jobs {
		out = append(out, copyJob(j))
	}
	return out
}

func copyJob(j *Job) Job {
	c := *j
	c.Files = append([]string{}, j.Files...)
	c.Details = append([]string{}, j.Details...)
	return c
}

// Add puts a new download into the list. Links from outside (a web page)
// wait for the user's OK: a page must not be able to start downloads.
func (q *queue) Add(link string, o DownloadOptions, source, referer, title, cookieFile string) (Job, error) {
	return q.AddWith(link, o, source, referer, title, cookieFile, jobExtras{})
}

// AddWith is Add for a link from the browser extension, with what it knows
// about the page and the request.
func (q *queue) AddWith(link string, o DownloadOptions, source, referer, title, cookieFile string, x jobExtras) (Job, error) {
	u, err := checkURL(link)
	if err != nil {
		return Job{}, err
	}
	if referer != "" {
		if referer, err = checkURL(referer); err != nil {
			referer = "" // a broken referer is not worth refusing the link for
		}
	}
	if err := o.validate(); err != nil {
		return Job{}, err
	}
	q.mu.Lock()
	q.counter++
	j := &Job{ID: fmt.Sprintf("%x-%d", time.Now().UnixNano(), q.counter), URL: u, Title: title,
		Source: source, Referer: referer, Options: o, Status: jobWaiting, Stage: "waiting",
		Added: time.Now(), Files: []string{}, Details: []string{}, ETA: -1, cookieFile: cookieFile,
		headerFile: x.HeaderFile, UserAgent: x.UserAgent}
	if cookieFile != "" && filepath.Dir(cookieFile) == q.cookieDir {
		j.CookieName = filepath.Base(cookieFile)
	}
	if x.HeaderFile != "" && filepath.Dir(x.HeaderFile) == q.cookieDir {
		j.HeaderName = filepath.Base(x.HeaderFile)
	}
	for _, f := range x.ListFiles {
		if filepath.Dir(f) == q.cookieDir {
			j.listFiles, j.ListNames = append(j.listFiles, f), append(j.ListNames, filepath.Base(f))
		}
	}
	if page, perr := checkURL(x.PageURL); perr == nil && page != u {
		j.PageURL = page
	}
	if source == "protocol" {
		j.Status, j.Stage = jobConfirm, "waiting for your OK"
	}
	q.jobs = append(q.jobs, j)
	q.trimLocked()
	q.saveLocked()
	c := copyJob(j)
	q.mu.Unlock()
	q.logger.Info("job added", "id", j.ID, "source", source, "host", hostOf(u))
	q.notify(c)
	q.Schedule()
	return c, nil
}

// trimLocked drops the oldest finished jobs beyond maxHistory.
func (q *queue) trimLocked() {
	finished := 0
	for _, j := range q.jobs {
		if isFinished(j.Status) {
			finished++
		}
	}
	keep := q.jobs[:0]
	for _, j := range q.jobs {
		if finished > maxHistory && isFinished(j.Status) {
			finished--
			continue
		}
		keep = append(keep, j)
	}
	q.jobs = keep
}

func isFinished(s string) bool { return s == jobDone || s == jobFailed || s == jobCancelled }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// removeOrphans deletes part folders of jobs that are gone or finished, and
// cookie files no unfinished job uses. The extension's newest cookie files
// may belong to a request that is still on its way (intake.go), so only
// files older than a minute go.
func (q *queue) removeOrphans() {
	q.mu.Lock()
	keepParts, keepCookies := map[string]bool{}, map[string]bool{}
	for _, j := range q.jobs {
		if !isFinished(j.Status) {
			keepParts[j.ID] = true
			for _, f := range append([]string{j.cookieFile, j.headerFile}, j.listFiles...) {
				if f != "" {
					keepCookies[filepath.Base(f)] = true
				}
			}
		}
	}
	q.mu.Unlock()
	if entries, err := os.ReadDir(q.tempRoot); err == nil {
		for _, e := range entries {
			if e.IsDir() && !keepParts[e.Name()] {
				os.RemoveAll(filepath.Join(q.tempRoot, e.Name())) // ignore: tried again next start
			}
		}
	}
	if entries, err := os.ReadDir(q.cookieDir); err == nil {
		for _, e := range entries {
			info, err := e.Info()
			if err != nil || keepCookies[e.Name()] || time.Since(info.ModTime()) < time.Minute {
				continue
			}
			os.Remove(filepath.Join(q.cookieDir, e.Name())) // ignore: tried again next start
		}
	}
}

func (q *queue) notify(j Job) {
	if q.onChange != nil {
		q.onChange(j)
	}
}

// SetParallel changes how many downloads run at once.
func (q *queue) SetParallel(n int) {
	q.mu.Lock()
	q.parallel = max(1, min(n, maxParallel))
	q.mu.Unlock()
	q.Schedule()
}

// Hold stops new starts (running ones go on) and says whether any job is
// still running; Release lets the queue go on.
func (q *queue) Hold() (running bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.hold = true
	return len(q.cancel) > 0
}

func (q *queue) Release() {
	q.mu.Lock()
	q.hold = false
	q.mu.Unlock()
	q.Schedule()
}

// Active is the number of downloads that run or wait to run. Paused jobs
// and links that wait for the user's OK do not count: they would not end
// by themselves.
func (q *queue) Active() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, j := range q.jobs {
		if j.Status == jobRunning || j.Status == jobWaiting {
			n++
		}
	}
	return n
}

// Running is the number of downloads in progress.
func (q *queue) Running() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.cancel)
}

// Schedule starts waiting jobs while there is room: first the ones the user
// put in front ("next"), then the oldest. A job that waits for a new try
// by itself is left out until its time has come.
func (q *queue) Schedule() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.hold || q.closed || !q.have("yt-dlp") {
		return
	}
	now := time.Now()
	var soonest time.Duration
	for _, wantNext := range []bool{true, false} {
		for _, j := range q.jobs {
			if len(q.cancel) >= q.parallel {
				return
			}
			if j.Status != jobWaiting || j.Next != wantNext {
				continue
			}
			if wait := j.RetryAt.Sub(now); wait > 0 {
				if soonest == 0 || wait < soonest {
					soonest = wait
				}
				continue
			}
			q.startLocked(j)
		}
	}
	if soonest > 0 {
		time.AfterFunc(soonest, q.Schedule) // one timer too many only looks at the list once more
	}
}

// startLocked lets a waiting job run; q.mu is held.
func (q *queue) startLocked(j *Job) {
	ctx, cancel := context.WithCancel(context.Background())
	q.cancel[j.ID] = cancel
	j.Status, j.Stage, j.Started, j.Error = jobRunning, "starting yt-dlp", time.Now(), ""
	if j.follow != "" {
		// A try of the job's own: what yt-dlp said before stays readable.
		j.Details = append(j.Details, "Next try: "+j.follow)
		j.follow = ""
	} else {
		j.Details, j.before = []string{}, nil
	}
	j.Speed, j.ETA, j.Files, j.Next, j.RetryAt = 0, -1, []string{}, false, time.Time{}
	j.Size, j.Skipped = 0, 0
	if j.Resumable {
		j.Stage = "continuing" // the numbers stay until yt-dlp reports its own
	} else {
		j.Percent, j.Done, j.Total = 0, 0, 0
	}
	q.saveLocked()
	c := copyJob(j)
	q.wg.Add(1)
	go q.run(ctx, j.ID, c)
}

// run is one yt-dlp process for one job.
func (q *queue) run(ctx context.Context, id string, j Job) {
	defer q.wg.Done()
	q.notify(j)
	temp := filepath.Join(q.tempRoot, id)
	var errLines []string
	jp := jobPaths{Temp: temp}
	var list string
	jp.Cookies, jp.Headers, list = q.loginFilesOf(id)
	if q.proxy != nil {
		jp.Proxy = q.proxy()
	}
	link, referer := j.URL, j.Referer
	if j.ViaPage {
		// The page instead of the link found on it: the login (cookies,
		// browser) still counts, what belonged to the request of the link
		// does not.
		link, referer, jp.Headers = j.PageURL, "", ""
	} else if list != "" {
		// A list the player built in memory: yt-dlp reads the file made of
		// it. The link of the job stays the player's page, for the list.
		link, jp.FileURLs = fileURL(list), true
	}
	if j.AsBrowser && !j.NoBrowser {
		jp.Impersonate = browserTarget(j.UserAgent)
	}
	args := buildArgs(j.Options, link, q.ts, q.have, jp, referer, j.UserAgent)
	q.logger.Info("download started", "id", id, "host", hostOf(j.URL))

	err := os.MkdirAll(temp, 0o700)
	if err == nil {
		hide := secretOf(jp.Proxy)
		err = q.r.Run(ctx, q.ts.YtDlp, args, func(_ bool, line string) {
			if hide != "" {
				// yt-dlp may name its proxy in an error; the password stays here.
				line = strings.ReplaceAll(line, hide, "***")
			}
			if list != "" {
				// Nor the folder of this user, which the files of a list are in.
				line = strings.ReplaceAll(strings.ReplaceAll(line, fileURL(q.cookieDir), "list"), q.cookieDir, "list")
			}
			l := parseLine(line)
			switch l.Kind {
			case lineError:
				errLines = append(errLines, l.Text)
			case lineOther:
				if line != "" {
					q.logger.Debug("yt-dlp", "id", id, "line", line)
				}
				return
			}
			q.update(id, l)
		})
	} else {
		err = fileError(err, "create the folder for unfinished parts", temp)
	}

	// The parts stay for a later run when the job was paused, VideoCatcher
	// ends, or the job tries again by itself; otherwise they go, however
	// the job ended.
	again := err != nil && ctx.Err() == nil && q.triesAgain(id, errLines)
	// Also after an error: a playlist of which one entry failed still has
	// its other files, and they get their names like the rest. Not before
	// another try: yt-dlp skips what is there by its own names.
	if ctx.Err() == nil && !again && j.Options.CleanNames {
		q.tidyFiles(id)
	}
	if !again && !q.keepsParts(id, ctx) {
		if rerr := os.RemoveAll(temp); rerr != nil {
			q.logger.Warn("cannot remove unfinished parts", "dir", temp, "err", rerr)
		}
	}
	q.finish(id, ctx, err, errLines, again)
}

// secretOf is the password in a proxy URL, or "".
func secretOf(proxy string) string {
	u, err := url.Parse(proxy)
	if err != nil || u.User == nil {
		return ""
	}
	pw, _ := u.User.Password() // ignore: no password, nothing to hide
	return pw
}

func (q *queue) keepsParts(id string, ctx context.Context) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return ctx.Err() != nil && (q.closing || q.pausing[id])
}

// triesAgain says whether a failed run gets another try by itself: the
// trouble was on the way to the site, and the tries are not used up.
func (q *queue) triesAgain(id string, errLines []string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	j := q.findLocked(id)
	return j != nil && !q.closing && j.Tries < len(q.delays) && worthAnotherTry(errLines)
}

// tidyFiles gives the downloaded files of a job their tidy names (names.go).
// A file that cannot be renamed keeps yt-dlp's name; the download counts.
func (q *queue) tidyFiles(id string) {
	q.mu.Lock()
	j := q.findLocked(id)
	if j == nil {
		q.mu.Unlock()
		return
	}
	files := append([]string{}, j.Files...)
	q.mu.Unlock()
	for i, f := range files {
		n, err := tidyFile(f)
		if err != nil {
			q.logger.Warn("file keeps its name", "id", id, "err", err)
		}
		files[i] = n
	}
	q.mu.Lock()
	if j := q.findLocked(id); j != nil {
		j.Files = files
	}
	q.mu.Unlock()
}

func (q *queue) loginFilesOf(id string) (cookies, headers, list string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if j := q.findLocked(id); j != nil {
		if len(j.listFiles) > 0 {
			list = j.listFiles[0]
		}
		return j.cookieFile, j.headerFile, list
	}
	return "", "", ""
}

func (q *queue) findLocked(id string) *Job {
	for _, j := range q.jobs {
		if j.ID == id {
			return j
		}
	}
	return nil
}

// update applies one line of yt-dlp's output to the job and tells the
// window, at most every updateInterval unless something important changed.
func (q *queue) update(id string, l ytdlpLine) {
	q.mu.Lock()
	j := q.findLocked(id)
	if j == nil {
		q.mu.Unlock()
		return
	}
	important := false
	switch l.Kind {
	case lineProgress:
		if l.Unreadable {
			break
		}
		j.Done, j.Total, j.Estimated, j.Speed, j.ETA = l.Done, l.Total, l.Estimated, l.Speed, l.ETA
		switch {
		case l.Total > 0:
			j.Percent = min(100, float64(l.Done)*100/float64(l.Total))
		case l.Fragments > 0:
			j.Percent = min(100, float64(l.Fragment)*100/float64(l.Fragments))
		}
		j.Stage = "downloading"
		if l.Status == "finished" {
			j.Percent, important = 100, true
		}
	case linePost:
		j.Stage, important = postStepLabel(l.PostStep), true
	case lineItem:
		j.Item, j.Items, j.ItemTitle, important = l.Index, l.Count, l.Title, true
		// A single video: its real title, unless the job has a name of its
		// own. A stream from the extension has, and yt-dlp only knows the
		// name of the stream file ("index-f3-v1-a1"); the page title is better.
		if l.Count == 0 && l.Title != "" && j.Options.FileName == "" {
			j.Title = l.Title
		}
		j.Percent, j.Done, j.Total = 0, 0, 0
	case lineFile:
		j.Files, important = append(j.Files, l.Path), true
	case lineError, lineWarning:
		prefix := "Error: "
		if l.Kind == lineWarning {
			prefix = "Warning: "
		}
		j.Details = append(j.Details, prefix+l.Text)
		if len(j.Details) > maxDetails {
			j.Details = j.Details[len(j.Details)-maxDetails:]
		}
		important = true
	}
	now := time.Now()
	if !important && now.Sub(q.lastSent[id]) < updateInterval {
		q.mu.Unlock()
		return
	}
	q.lastSent[id] = now
	c := copyJob(j)
	q.mu.Unlock()
	q.notify(c)
}

func (q *queue) finish(id string, ctx context.Context, err error, errLines []string, again bool) {
	q.mu.Lock()
	delete(q.cancel, id)
	delete(q.lastSent, id)
	j := q.findLocked(id)
	if j == nil {
		q.mu.Unlock()
		q.Schedule()
		return
	}
	paused := q.pausing[id]
	delete(q.pausing, id)
	j.Ended, j.Speed, j.ETA = time.Now(), 0, -1
	if !j.Started.IsZero() {
		j.Elapsed += j.Ended.Sub(j.Started).Seconds()
	}
	if ctx.Err() != nil && (q.closing || paused) {
		// Parts and cookies stay for the next run.
		j.Resumable = true
		if q.closing {
			j.Status, j.Stage = jobWaiting, "interrupted, continues"
		} else {
			j.Status, j.Stage = jobPaused, "paused"
			q.logger.Info("download paused", "id", id)
		}
		q.saveLocked()
		c := copyJob(j)
		q.mu.Unlock()
		q.notify(c)
		q.Schedule()
		return
	}
	if again && j.Tries < len(q.delays) {
		// Parts and cookies stay here too: the next try continues them.
		wait := q.delays[j.Tries]
		j.Tries++
		j.Status, j.Stage, j.Resumable, j.RetryAt = jobWaiting, "site not reached, trying again soon", true, time.Now().Add(wait)
		j.follow, j.before = "the same again", append(j.before, errLines...)
		q.logger.Warn("download tries again", "id", id, "host", hostOf(j.URL), "try", j.Tries, "in", wait.String(), "ytdlp", firstLine(strings.Join(errLines, "\n")))
		q.saveLocked()
		c := copyJob(j)
		q.mu.Unlock()
		q.notify(c)
		q.Schedule()
		return
	}
	failed := err != nil && ctx.Err() == nil && len(j.Files) == 0
	if failed && j.AsBrowser && !j.NoBrowser && cannotImpersonate(errLines) {
		// This yt-dlp has nothing to answer like a browser with. The try
		// did not reach the site, so what the site said before counts.
		j.NoBrowser = true
		q.logger.Warn("yt-dlp cannot act as a browser", "id", id, "ytdlp", firstLine(strings.Join(errLines, "\n")))
		errLines = append([]string{}, j.before...)
	}
	if failed && !j.AsBrowser && wantsBrowser(errLines) {
		// The site lets browsers in and turned yt-dlp away: once more,
		// answering like the browser the link comes from. Same link, same
		// login.
		j.AsBrowser, j.Tries = true, 0
		j.Status, j.Stage, j.Resumable = jobWaiting, "site wants a browser, trying as one", false
		j.follow, j.before = "as a browser", append(j.before, errLines...)
		q.logger.Warn("download tries as a browser", "id", id, "host", hostOf(j.URL), "ytdlp", firstLine(strings.Join(errLines, "\n")))
		q.saveLocked()
		c := copyJob(j)
		q.mu.Unlock()
		q.notify(c)
		q.Schedule()
		return
	}
	if failed && !j.ViaPage && j.PageURL != "" && linkRefused(errLines) {
		// A link with a token that is no longer good (it waited too long,
		// or it was made for the player alone): the page it was found on
		// is tried instead, once, with the same login.
		j.ViaPage, j.Tries = true, 0
		j.Status, j.Stage, j.Resumable = jobWaiting, "link refused, trying the page instead", false
		j.follow, j.before = "the page the video was found on", append(j.before, errLines...)
		q.logger.Warn("download tries the page instead", "id", id, "host", hostOf(j.URL), "page", hostOf(j.PageURL), "ytdlp", firstLine(strings.Join(errLines, "\n")))
		q.saveLocked()
		c := copyJob(j)
		q.mu.Unlock()
		q.notify(c)
		q.Schedule()
		return
	}
	q.dropCookiesLocked(j)
	j.Resumable = false
	switch {
	case ctx.Err() != nil:
		j.Status, j.Stage = jobCancelled, "stopped"
		q.logger.Info("download stopped", "id", id)
	case err != nil && j.Options.Playlist == "all" && len(j.Files) > 0:
		// A playlist of which some entries could not be loaded (private,
		// deleted, blocked): yt-dlp goes on with the rest and ends with an
		// error. What was downloaded counts; the entries that failed are
		// named in the details.
		j.Status, j.Stage, j.Percent = jobDone, "done, but not every video", 100
		j.Skipped, j.Tries = max(1, len(errLines)), 0
		j.Error = errorText(explainYtDlp(errLines, err))
		j.Size = sizeOf(j.Files)
		q.logger.Warn("playlist done with errors", "id", id, "host", hostOf(j.URL), "files", len(j.Files), "errors", len(errLines))
	case err != nil:
		uerr := explainYtDlp(errLines, err)
		var ue *UserError
		if errors.As(err, &ue) && len(errLines) == 0 {
			uerr = err // our own error (folder, start of yt-dlp), already explained
		} else {
			uerr = explainEnd(j.before, errLines, err, j.ViaPage, j.NoBrowser)
		}
		if said := strings.Join(errLines, "\n"); len(j.ListNames) > 0 && strings.Contains(strings.ToLower(said), "invalid data found") {
			// All parts came, but they are not video: locked by the player.
			uerr = userError(errors.New(said), lockedMsg, lockedHint)
		}
		j.Status, j.Stage, j.Error = jobFailed, "failed", errorText(uerr)
		q.logger.Error("download failed", "id", id, "host", hostOf(j.URL), "err", err, "ytdlp", firstLine(strings.Join(errLines, "\n")))
	default:
		j.Status, j.Stage, j.Percent, j.Tries = jobDone, "done", 100, 0
		if len(j.Files) == 0 {
			j.Stage = "done (the file was already there)"
		}
		j.Size = sizeOf(j.Files)
		q.logger.Info("download done", "id", id, "files", len(j.Files))
	}
	q.saveLocked()
	c := copyJob(j)
	q.mu.Unlock()
	q.notify(c)
	q.Schedule()
}

// sizeOf adds up the sizes of the files a job downloaded. A file that is
// gone already (moved by the user) counts nothing.
func sizeOf(files []string) int64 {
	var n int64
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && info.Mode().IsRegular() {
			n += info.Size()
		}
	}
	return n
}

// dropCookiesLocked deletes the logins of a job, its cookie file and its
// file with request headers; q.mu is held.
func (q *queue) dropCookiesLocked(j *Job) {
	for _, f := range append([]string{j.cookieFile, j.headerFile}, j.listFiles...) {
		if f == "" {
			continue
		}
		if rerr := os.Remove(f); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			q.logger.Warn("cannot remove a cookie file", "err", rerr)
		}
	}
	j.cookieFile, j.headerFile, j.CookieName, j.HeaderName = "", "", "", ""
	j.listFiles, j.ListNames = nil, nil
}

// dropPartsLocked deletes the kept parts of a job that will not continue;
// q.mu is held, the job does not run.
func (q *queue) dropPartsLocked(j *Job) {
	if err := os.RemoveAll(filepath.Join(q.tempRoot, j.ID)); err != nil {
		q.logger.Warn("cannot remove unfinished parts", "id", j.ID, "err", err)
	}
	j.Resumable = false
}

// Pause stops a running job and keeps its parts, or holds a waiting one.
// "Continue" (Resume) goes on where it stopped.
func (q *queue) Pause(id string) error {
	q.mu.Lock()
	j := q.findLocked(id)
	if j == nil {
		q.mu.Unlock()
		return userError(errors.New("unknown job "+id), "This download is no longer in the list", "")
	}
	if cancel, ok := q.cancel[id]; ok {
		q.pausing[id] = true
		q.mu.Unlock()
		cancel() // run() ends and calls finish, which sees pausing
		return nil
	}
	changed := j.Status == jobWaiting
	if changed {
		j.Status, j.Stage = jobPaused, "paused"
		q.saveLocked()
	}
	c := copyJob(j)
	q.mu.Unlock()
	if changed {
		q.notify(c)
	}
	return nil
}

// Resume lets a paused job wait for its turn again.
func (q *queue) Resume(id string) error {
	return q.setStatus(id, func(j *Job) bool {
		if j.Status != jobPaused {
			return false
		}
		j.Status, j.Stage, j.RetryAt = jobWaiting, "waiting", time.Time{}
		return true
	})
}

// PauseAll pauses every running and waiting job; ResumeAll lets every
// paused one go on.
func (q *queue) PauseAll() {
	for _, id := range q.idsWith(jobRunning, jobWaiting) {
		q.Pause(id) // ignore: a job that ended in between needs no pause
	}
}

func (q *queue) ResumeAll() {
	for _, id := range q.idsWith(jobPaused) {
		q.Resume(id) // ignore: a job removed in between needs nothing
	}
}

func (q *queue) idsWith(states ...string) []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	var ids []string
	for _, j := range q.jobs {
		for _, s := range states {
			if j.Status == s {
				ids = append(ids, j.ID)
			}
		}
	}
	return ids
}

// Cancel stops a running job or takes a waiting one out of the line.
func (q *queue) Cancel(id string) error {
	q.mu.Lock()
	j := q.findLocked(id)
	if j == nil {
		q.mu.Unlock()
		return userError(errors.New("unknown job "+id), "This download is no longer in the list", "")
	}
	if cancel, ok := q.cancel[id]; ok {
		q.mu.Unlock()
		cancel() // run() ends and calls finish
		return nil
	}
	if j.Status == jobWaiting || j.Status == jobConfirm || j.Status == jobPaused {
		j.Status, j.Stage = jobCancelled, "stopped"
		q.dropPartsLocked(j)
		q.dropCookiesLocked(j)
		q.saveLocked()
	}
	c := copyJob(j)
	q.mu.Unlock()
	q.notify(c)
	return nil
}

// Retry puts a failed or stopped job back into the line.
func (q *queue) Retry(id string) error {
	return q.setStatus(id, func(j *Job) bool {
		if !isFinished(j.Status) {
			return false
		}
		q.againLocked(j)
		return true
	})
}

// againLocked puts a finished job back into the line; q.mu is held.
func (q *queue) againLocked(j *Job) {
	j.Status, j.Stage, j.Error, j.Percent, j.Done, j.Total = jobWaiting, "waiting", "", 0, 0, 0
	j.Tries, j.RetryAt, j.Elapsed, j.Size, j.Skipped = 0, time.Time{}, 0, 0, 0
	j.follow, j.before = "", nil
	q.dropPartsLocked(j) // a new try starts clean
}

// RetryFailed puts every failed job back into the line and says how many.
func (q *queue) RetryFailed() int {
	q.mu.Lock()
	var again []Job
	for _, j := range q.jobs {
		if j.Status == jobFailed {
			q.againLocked(j)
			again = append(again, copyJob(j))
		}
	}
	if len(again) > 0 {
		q.saveLocked()
	}
	q.mu.Unlock()
	for _, c := range again {
		q.notify(c)
	}
	q.Schedule()
	return len(again)
}

// StartNext puts a waiting job in front of the other waiting ones: it gets
// the next free place. A job that waits for a new try by itself tries now.
func (q *queue) StartNext(id string) error {
	return q.setStatus(id, func(j *Job) bool {
		if j.Status != jobWaiting {
			return false
		}
		j.Next, j.RetryAt, j.Stage = true, time.Time{}, "next"
		return true
	})
}

// Known finds the newest job with this link that is not failed or stopped:
// one that still waits or runs, or one that is done. The window asks
// before it adds a link a second time.
func (q *queue) Known(link string) (Job, bool) {
	u, err := checkURL(link)
	if err != nil {
		return Job{}, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := len(q.jobs) - 1; i >= 0; i-- {
		if j := q.jobs[i]; j.URL == u && j.Status != jobFailed && j.Status != jobCancelled {
			return copyJob(j), true
		}
	}
	return Job{}, false
}

// Progress is what the window title shows: how many downloads run, and how
// far they are on average.
func (q *queue) Progress() (running int, percent float64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, j := range q.jobs {
		if j.Status == jobRunning {
			running++
			percent += j.Percent
		}
	}
	if running > 0 {
		percent /= float64(running)
	}
	return running, percent
}

// Confirm lets a job from outside start.
func (q *queue) Confirm(id string) error {
	return q.setStatus(id, func(j *Job) bool {
		if j.Status != jobConfirm {
			return false
		}
		j.Status, j.Stage = jobWaiting, "waiting"
		return true
	})
}

func (q *queue) setStatus(id string, change func(j *Job) bool) error {
	q.mu.Lock()
	j := q.findLocked(id)
	if j == nil {
		q.mu.Unlock()
		return userError(errors.New("unknown job "+id), "This download is no longer in the list", "")
	}
	changed := change(j)
	if changed {
		q.saveLocked()
	}
	c := copyJob(j)
	q.mu.Unlock()
	if changed {
		q.notify(c)
		q.Schedule()
	}
	return nil
}

// Remove takes jobs out of the list; running ones are stopped first by the
// caller. Only the entry goes, never a downloaded file.
func (q *queue) Remove(ids ...string) {
	q.mu.Lock()
	drop := map[string]bool{}
	for _, id := range ids {
		if _, running := q.cancel[id]; !running {
			drop[id] = true
		}
	}
	keep := q.jobs[:0]
	for _, j := range q.jobs {
		if drop[j.ID] {
			q.dropCookiesLocked(j)
			if !isFinished(j.Status) {
				q.dropPartsLocked(j)
			}
			continue
		}
		keep = append(keep, j)
	}
	q.jobs = keep
	q.saveLocked()
	q.mu.Unlock()
}

// FinishedIDs lists the jobs that are done, failed or stopped.
func (q *queue) FinishedIDs() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	var ids []string
	for _, j := range q.jobs {
		if isFinished(j.Status) {
			ids = append(ids, j.ID)
		}
	}
	return ids
}

// Close stops every running download and waits for them to end. They are
// kept as waiting with their parts and continue with the next start of
// VideoCatcher.
func (q *queue) Close() {
	q.mu.Lock()
	q.closing, q.hold = true, true
	for _, c := range q.cancel {
		c()
	}
	q.mu.Unlock()
	q.wg.Wait()
	q.mu.Lock()
	q.saveLocked()
	q.closed = true
	q.mu.Unlock()
}
