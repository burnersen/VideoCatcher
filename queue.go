// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// The download manager: a list of jobs, of which at most "parallel" run at
// the same time. Each job is one run of yt-dlp. The list is saved in
// queue.json after every change of state, so it survives a restart; a job
// that was running when VideoCatcher ended starts again from the beginning
// (yt-dlp continues a half download where it can).
//
// Unfinished parts live in a folder of the job below the program's data
// folder, never in the download folder; it is removed when the job ends,
// however it ends (README promise: no half files in your folder).

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
	r        runner
	ts       toolSet
	have     func(tool string) bool
	tempRoot string // parts of unfinished downloads
	file     string // queue.json
	logger   *slog.Logger
	onChange func(Job) // the window; may be nil

	mu       sync.Mutex
	jobs     []*Job
	cancel   map[string]context.CancelFunc
	parallel int
	hold     bool // no new starts, e.g. while yt-dlp is being replaced
	counter  int
	lastSent map[string]time.Time
	wg       sync.WaitGroup
	closing  bool // stopping because VideoCatcher ends: running jobs wait for the next start
	closed   bool
}

func newQueue(r runner, ts toolSet, have func(string) bool, dataDir string, logger *slog.Logger) *queue {
	return &queue{r: r, ts: ts, have: have, logger: logger, parallel: 2,
		tempRoot: filepath.Join(dataDir, "partial"), file: filepath.Join(dataDir, "queue.json"),
		cancel: map[string]context.CancelFunc{}, lastSent: map[string]time.Time{}}
}

type queueFile struct {
	Format int    `json:"format"`
	Jobs   []*Job `json:"jobs"`
}

// load reads queue.json. Running jobs of the last session wait again.
func (q *queue) load() error {
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
			j.Status, j.Stage = jobWaiting, "interrupted, starts again"
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
		j.Percent, j.Done, j.Total, j.Speed, j.ETA, j.Files = 0, 0, 0, 0, -1, []string{}
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
	args := buildArgs(j.Options, j.URL, q.ts, q.have, jobPaths{Temp: temp, Cookies: q.cookieFileOf(id)}, j.Referer)
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

	// However it ended: the unfinished parts and a cookie file go.
	if rerr := os.RemoveAll(temp); rerr != nil {
		q.logger.Warn("cannot remove unfinished parts", "dir", temp, "err", rerr)
	}
	q.finish(id, ctx, err, errLines)
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
		if l.Count == 0 && l.Title != "" {
			j.Title = l.Title // a single video: its real title
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
	if j.cookieFile != "" {
		if rerr := os.Remove(j.cookieFile); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			q.logger.Warn("cannot remove a cookie file", "err", rerr)
		}
		j.cookieFile = ""
	}
	j.Ended, j.Speed, j.ETA = time.Now(), 0, -1
	switch {
	case ctx.Err() != nil && q.closing:
		j.Status, j.Stage = jobWaiting, "interrupted, starts again"
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
	if j.Status == jobWaiting || j.Status == jobConfirm {
		j.Status, j.Stage = jobCancelled, "stopped"
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
		j.Status, j.Stage, j.Error, j.Percent = jobWaiting, "waiting", "", 0
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
			if j.cookieFile != "" {
				os.Remove(j.cookieFile) // ignore: best effort, the file has no use any more
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
// kept as waiting and start again with the next start of VideoCatcher.
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
