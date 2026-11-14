// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

// The speed slider (1 to 100 %). yt-dlp can only be given a speed limit when
// it starts; to change it while downloads run, yt-dlp talks to the net
// through a small relay inside VideoCatcher (--proxy), and the relay passes
// on only as many bytes per second as the slider allows, for all downloads
// together.
//
// The relay
//
//   - listens on 127.0.0.1 only, on a port the system picks, and only for
//     this run of VideoCatcher
//   - answers only requests with its password, a random one per start that
//     only yt-dlp gets; anything else gets 407 and is never forwarded
//   - does not look into https: a CONNECT tunnel carries the encrypted bytes
//     through, yt-dlp checks the certificates itself
//   - adds nothing to plain http requests (no X-Forwarded-For)
//
// 100 % is the fastest speed measured while there was no limit, so the
// slider means "share of my line". Until something was measured there is
// nothing to take a share of, and the slider has no effect.

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// Limits and timings (05 K9).
const (
	relayChunk      = 16 << 10 // bytes passed per step; small enough for a smooth low limit
	minBurst        = 64 << 10 // tokens that may pile up: at least a few chunks, or a slow limit would never let one through
	maxWaitStep     = 100 * time.Millisecond
	speedWindow     = 3 // seconds the measured speed is averaged over
	relayDialTime   = 30 * time.Second
	minSpeedPercent = 1
	maxSpeedPercent = 100
)

// limiter is a token bucket shared by every connection of the relay.
type limiter struct {
	mu     sync.Mutex
	rate   float64 // bytes per second; 0 means no limit
	tokens float64
	last   time.Time
	now    func() time.Time
	sleep  func(time.Duration)
}

// relayDialPauses are the waits between the attempts to reach a site.
var relayDialPauses = []time.Duration{500 * time.Millisecond, 2 * time.Second, 5 * time.Second}

func newLimiter() *limiter {
	return &limiter{now: time.Now, sleep: time.Sleep}
}

// SetRate changes the limit at once, also for bytes that wait right now.
func (l *limiter) SetRate(bytesPerSecond float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rate = max(0, bytesPerSecond)
	l.tokens = min(l.tokens, l.burstLocked())
}

func (l *limiter) Rate() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rate
}

func (l *limiter) burstLocked() float64 { return max(l.rate/4, minBurst) }

// wait blocks until n bytes may pass. It waits in short steps, so a new
// limit counts within a tenth of a second.
func (l *limiter) wait(ctx context.Context, n int) {
	for {
		l.mu.Lock()
		if l.rate <= 0 {
			l.mu.Unlock()
			return
		}
		now := l.now()
		if !l.last.IsZero() {
			l.tokens = min(l.burstLocked(), l.tokens+now.Sub(l.last).Seconds()*l.rate)
		}
		l.last = now
		if l.tokens >= float64(n) {
			l.tokens -= float64(n)
			l.mu.Unlock()
			return
		}
		need := time.Duration((float64(n) - l.tokens) / l.rate * float64(time.Second))
		l.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		l.sleep(min(need, maxWaitStep))
	}
}

// meter counts the bytes that came in and gives the speed of the last few
// seconds.
type meter struct {
	total   atomic.Int64
	mu      sync.Mutex
	samples []int64 // total at each of the last ticks, newest last
}

func (m *meter) add(n int) { m.total.Add(int64(n)) }

// tick is called once a second and returns the bytes per second, averaged
// over the last speedWindow seconds.
func (m *meter) tick() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.samples = append(m.samples, m.total.Load())
	if len(m.samples) > speedWindow+1 {
		m.samples = m.samples[len(m.samples)-speedWindow-1:]
	}
	if len(m.samples) < 2 {
		return 0
	}
	return float64(m.samples[len(m.samples)-1]-m.samples[0]) / float64(len(m.samples)-1)
}

// relay is the local proxy yt-dlp uses.
type relay struct {
	ln        net.Listener
	srv       *http.Server
	auth      string // the whole Proxy-Authorization value that is accepted
	url       string // what yt-dlp gets as --proxy
	lim       *limiter
	meter     *meter
	transport *http.Transport
	logger    *slog.Logger
	dial      func(ctx context.Context, network, addr string) (net.Conn, error)

	mu     sync.Mutex
	conns  map[net.Conn]bool // hijacked tunnels, closed on shutdown
	ctx    context.Context
	cancel context.CancelFunc
}

func startRelay(lim *limiter, m *meter, logger *slog.Logger) (*relay, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		ln.Close() // ignore: the error that counts is the one above
		return nil, err
	}
	pass := hex.EncodeToString(secret)
	d := &net.Dialer{Timeout: relayDialTime, KeepAlive: 30 * time.Second}
	r := &relay{ln: ln, lim: lim, meter: m, logger: logger, dial: d.DialContext, conns: map[net.Conn]bool{},
		auth: "Basic " + base64.StdEncoding.EncodeToString([]byte("vc:"+pass)),
		url:  (&url.URL{Scheme: "http", User: url.UserPassword("vc", pass), Host: ln.Addr().String()}).String()}
	r.ctx, r.cancel = context.WithCancel(context.Background())
	// No proxy behind the relay: it is the way out itself. No compression
	// added: yt-dlp asks for what it wants.
	r.transport = &http.Transport{Proxy: nil, DialContext: r.dial, DisableCompression: true,
		ForceAttemptHTTP2: false, MaxIdleConns: 20, IdleConnTimeout: 90 * time.Second,
		TLSHandshakeTimeout: relayDialTime, ResponseHeaderTimeout: 2 * time.Minute}
	r.srv = &http.Server{Handler: r, ReadHeaderTimeout: relayDialTime, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelDebug)}
	go r.srv.Serve(ln) // ignore: ends with ErrServerClosed when VideoCatcher closes
	return r, nil
}

// URL is the --proxy value for yt-dlp, with the password.
func (r *relay) URL() string { return r.url }

// Close stops the relay and every tunnel that is still open.
func (r *relay) Close() {
	r.cancel()
	r.srv.Close() // ignore: closing anyway
	r.mu.Lock()
	for c := range r.conns {
		c.Close() // ignore: closing anyway
	}
	r.mu.Unlock()
	r.transport.CloseIdleConnections()
}

func (r *relay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// The password first: nothing else is looked at for a request without it.
	if subtle.ConstantTimeCompare([]byte(req.Header.Get("Proxy-Authorization")), []byte(r.auth)) != 1 {
		w.Header().Set("Proxy-Authenticate", `Basic realm="VideoCatcher"`)
		http.Error(w, "proxy password needed", http.StatusProxyAuthRequired)
		return
	}
	switch {
	case req.Method == http.MethodConnect:
		r.tunnel(w, req)
	case req.URL.IsAbs() && (req.URL.Scheme == "http" || req.URL.Scheme == "https"):
		r.forward(w, req)
	default:
		http.Error(w, "not a proxy request", http.StatusBadRequest)
	}
}

// tunnel carries an https connection: bytes from the site go through the
// limiter, bytes to the site do not (they are small requests).
func (r *relay) tunnel(w http.ResponseWriter, req *http.Request) {
	if _, _, err := net.SplitHostPort(req.Host); err != nil {
		http.Error(w, "CONNECT needs host:port", http.StatusBadRequest)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "cannot tunnel", http.StatusInternalServerError)
		return
	}
	site, err := r.dialPatiently(req.Context(), req.Host)
	if err != nil {
		r.logger.Warn("relay: cannot reach the site", "host", req.Host, "err", err)
		http.Error(w, "cannot reach the site", http.StatusBadGateway)
		return
	}
	client, buf, err := hj.Hijack()
	if err != nil {
		site.Close() // ignore: the request failed anyway
		return
	}
	r.track(client, site)
	defer r.untrack(client, site)
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n")); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() {
		// Bytes the client sent along with the CONNECT are in buf already.
		io.Copy(site, io.MultiReader(io.LimitReader(buf, int64(buf.Reader.Buffered())), client)) // ignore: ends when either side closes
		closeWrite(site)
		done <- struct{}{}
	}()
	go func() {
		r.copyLimited(client, site)
		closeWrite(client)
		done <- struct{}{}
	}()
	<-done
	<-done
}

// dialPatiently tries a few times before it gives up. yt-dlp repeats a
// failed fragment at once, so one short hiccup (a name lookup that fails, a
// server that refuses a moment) would otherwise use up all its retries.
func (r *relay) dialPatiently(ctx context.Context, host string) (net.Conn, error) {
	var err error
	for _, pause := range relayDialPauses {
		var c net.Conn
		if c, err = r.dial(ctx, "tcp", host); err == nil {
			return c, nil
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(pause):
		}
	}
	return r.dial(ctx, "tcp", host)
}

func closeWrite(c net.Conn) {
	if tc, ok := c.(*net.TCPConn); ok {
		tc.CloseWrite() // ignore: the other side may be gone already
		return
	}
	c.Close() // ignore: the other side may be gone already
}

// copyLimited copies from the site to yt-dlp at the allowed speed. Reading
// in small steps and waiting before the next read lets TCP slow the site
// down; nothing piles up in memory.
func (r *relay) copyLimited(dst io.Writer, src io.Reader) {
	buf := make([]byte, relayChunk)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			r.lim.wait(r.ctx, n)
			r.meter.add(n)
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// forward passes a plain http request on. httputil.ReverseProxy removes the
// hop-by-hop headers (with Proxy-Authorization), streams the answer and
// adds no X-Forwarded headers when Rewrite is used without SetXForwarded.
func (r *relay) forward(w http.ResponseWriter, req *http.Request) {
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			u := *pr.In.URL
			pr.Out.URL = &u
			pr.Out.Host = pr.In.Host
		},
		Transport:     limitedTransport{r},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			r.logger.Debug("relay: request failed", "err", err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	rp.ServeHTTP(w, req)
}

type limitedTransport struct{ r *relay }

func (t limitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.r.transport.RoundTrip(req)
	// An upgraded connection (101, a websocket) must stay as it is:
	// ReverseProxy needs its body to be readable and writable.
	if err == nil && resp.Body != nil && resp.StatusCode != http.StatusSwitchingProtocols {
		resp.Body = &limitedBody{rc: resp.Body, r: t.r}
	}
	return resp, err
}

type limitedBody struct {
	rc io.ReadCloser
	r  *relay
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if len(p) > relayChunk {
		p = p[:relayChunk]
	}
	n, err := b.rc.Read(p)
	if n > 0 {
		b.r.lim.wait(b.r.ctx, n)
		b.r.meter.add(n)
	}
	return n, err
}

func (b *limitedBody) Close() error { return b.rc.Close() }

func (r *relay) track(cs ...net.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range cs {
		r.conns[c] = true
	}
}

func (r *relay) untrack(cs ...net.Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range cs {
		c.Close() // ignore: done with it
		delete(r.conns, c)
	}
}

// SpeedState is what the window shows next to the slider.
type SpeedState struct {
	Percent int     `json:"percent"` // the slider
	Rate    float64 `json:"rate"`    // bytes per second right now, all downloads
	Limit   float64 `json:"limit"`   // bytes per second allowed; 0 = no limit
	Peak    float64 `json:"peak"`    // 100 %: fastest speed measured without a limit; 0 = not yet
	Active  bool    `json:"active"`  // the relay runs; false: the slider cannot work
}

// speedControl ties slider, measurement and relay together.
type speedControl struct {
	lim   *limiter
	meter *meter
	relay *relay // nil if it could not start

	mu      sync.Mutex
	percent int
	peak    float64
	rate    float64
}

func newSpeedControl(percent int, peak float64, logger *slog.Logger) *speedControl {
	s := &speedControl{lim: newLimiter(), meter: &meter{}, percent: clampPercent(percent), peak: max(0, peak)}
	r, err := startRelay(s.lim, s.meter, logger)
	if err != nil {
		logger.Warn("speed relay not started, downloads go direct", "err", err)
	} else {
		s.relay = r
	}
	s.applyLocked()
	return s
}

func clampPercent(p int) int { return max(minSpeedPercent, min(p, maxSpeedPercent)) }

// ProxyURL is the --proxy value for yt-dlp, or "" without a relay.
func (s *speedControl) ProxyURL() string {
	if s.relay == nil {
		return ""
	}
	return s.relay.URL()
}

func (s *speedControl) SetPercent(p int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.percent = clampPercent(p)
	s.applyLocked()
}

// applyLocked sets the limiter from slider and peak; s.mu is held.
func (s *speedControl) applyLocked() {
	if s.percent >= maxSpeedPercent || s.peak <= 0 {
		s.lim.SetRate(0)
		return
	}
	s.lim.SetRate(s.peak * float64(s.percent) / 100)
}

// Tick measures (once a second). It returns the state for the window and
// whether the peak rose, so the caller can save it.
func (s *speedControl) Tick() (SpeedState, bool) {
	rate := s.meter.tick()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rate = rate
	rose := false
	// Only a speed without a limit says what the line can do.
	if s.lim.Rate() == 0 && rate > s.peak*1.02 {
		s.peak, rose = rate, true
	}
	return s.stateLocked(), rose
}

func (s *speedControl) State() SpeedState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()
}

func (s *speedControl) stateLocked() SpeedState {
	return SpeedState{Percent: s.percent, Rate: s.rate, Limit: s.lim.Rate(), Peak: s.peak, Active: s.relay != nil}
}

func (s *speedControl) Close() {
	if s.relay != nil {
		s.relay.Close()
	}
}
