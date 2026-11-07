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
	// CookieName is the name of the job's cookie file in the cookie folder,
	// so a paused job still has its login after a restart. Only the name:
	// the cookies themselves never go into queue.json.
	CookieName string `json:"cookieName,omitempty"`

	cookieFile string // from the browser extension; deleted when the job ends
}

// Limits (05 K9).
const (
	maxDetails     = 40                     // yt-dlp lines kept per job; the log has all
	maxHistory     = 500                    // finished jobs kept in the list
	updateInterval = 250 * time.Millisecond // progress to the window, per job
	queueFormat    = 1
)

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
	hold     bool // no new starts, e.g. while yt-dlp is being replaced
	counter  int
	lastSent map[string]time.Time
	wg       sync.WaitGroup
	closing  bool            // stopping because VideoCatcher ends: running jobs wait for the next start
	pausing  map[string]bool // running jobs the user paused; they end as paused, not stopped
	closed   bool
}

func newQueue(r runner, ts toolSet, have func(string) bool, dataDir string, logger *slog.Logger) *queue {
	return &queue{r: r, ts: ts, have: have, logger: logger, parallel: 2,
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
		if name := j.CookieName; name != "" && !isFinished(j.Status) {
			if p := filepath.Join(q.cookieDir, name); filepath.Base(name) == name && fileExists(p) {
				j.cookieFile = p
			}
		}
		q.jobs = append(q.jobs, j)
	}
	return nil
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
		Added: time.Now(), Files: []string{}, Details: []string{}, ETA: -1, cookieFile: cookieFile}
	if cookieFile != "" && filepath.Dir(cookieFile) == q.cookieDir {
		j.CookieName = filepath.Base(cookieFile)
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
			if j.cookieFile != "" {
				keepCookies[filepath.Base(j.cookieFile)] = true
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

// Schedule starts waiting jobs while there is room.
func (q *queue) Schedule() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.hold || q.closed || !q.have("yt-dlp") {
		return
	}
	for _, j := range q.jobs {
		if len(q.cancel) >= q.parallel {
			return
		}
		if j.Status != jobWaiting {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		q.cancel[j.ID] = cancel
		j.Status, j.Stage, j.Started, j.Error, j.Details = jobRunning, "starting yt-dlp", time.Now(), "", []string{}
		j.Speed, j.ETA, j.Files = 0, -1, []string{}
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
}

// run is one yt-dlp process for one job.
func (q *queue) run(ctx context.Context, id string, j Job) {
	defer q.wg.Done()
	q.notify(j)
	temp := filepath.Join(q.tempRoot, id)
	var errLines []string
	jp := jobPaths{Temp: temp, Cookies: q.cookieFileOf(id)}
	if q.proxy != nil {
		jp.Proxy = q.proxy()
	}
	args := buildArgs(j.Options, j.URL, q.ts, q.have, jp, j.Referer)
	q.logger.Info("download started", "id", id, "host", hostOf(j.URL))

	err := os.MkdirAll(temp, 0o700)
	if err == nil {
		err = q.r.Run(ctx, q.ts.YtDlp, args, func(_ bool, line string) {
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

	if err == nil && ctx.Err() == nil && j.Options.CleanNames {
		q.tidyFiles(id)
	}
	// The parts stay for a later run when the job was paused or VideoCatcher
	// ends; otherwise they go, however the job ended.
	if !q.keepsParts(id, ctx) {
		if rerr := os.RemoveAll(temp); rerr != nil {
			q.logger.Warn("cannot remove unfinished parts", "dir", temp, "err", rerr)
		}
	}
	q.finish(id, ctx, err, errLines)
}

func (q *queue) keepsParts(id string, ctx context.Context) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return ctx.Err() != nil && (q.closing || q.pausing[id])
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

func (q *queue) cookieFileOf(id string) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if j := q.findLocked(id); j != nil {
		return j.cookieFile
	}
	return ""
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

func (q *queue) finish(id string, ctx context.Context, err error, errLines []string) {
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
	q.dropCookiesLocked(j)
	j.Resumable = false
	switch {
	case ctx.Err() != nil:
		j.Status, j.Stage = jobCancelled, "stopped"
		q.logger.Info("download stopped", "id", id)
	case err != nil:
		uerr := explainYtDlp(errLines, err)
		var ue *UserError
		if errors.As(err, &ue) && len(errLines) == 0 {
			uerr = err // our own error (folder, start of yt-dlp), already explained
		}
		j.Status, j.Stage, j.Error = jobFailed, "failed", errorText(uerr)
		q.logger.Error("download failed", "id", id, "host", hostOf(j.URL), "err", err, "ytdlp", firstLine(strings.Join(errLines, "\n")))
	default:
		j.Status, j.Stage, j.Percent = jobDone, "done", 100
		if len(j.Files) == 0 {
			j.Stage = "done (the file was already there)"
		}
		q.logger.Info("download done", "id", id, "files", len(j.Files))
	}
	q.saveLocked()
	c := copyJob(j)
	q.mu.Unlock()
	q.notify(c)
	q.Schedule()
}

// dropCookiesLocked deletes the cookie file of a job; q.mu is held.
func (q *queue) dropCookiesLocked(j *Job) {
	if j.cookieFile != "" {
		if rerr := os.Remove(j.cookieFile); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			q.logger.Warn("cannot remove a cookie file", "err", rerr)
		}
		j.cookieFile = ""
	}
	j.CookieName = ""
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
		j.Status, j.Stage = jobWaiting, "waiting"
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
		j.Status, j.Stage, j.Error, j.Percent, j.Done, j.Total = jobWaiting, "waiting", "", 0, 0, 0
		q.dropPartsLocked(j) // a new try starts clean
		return true
	})
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
