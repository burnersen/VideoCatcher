// VideoCatcher (https://github.com/burnersen/VideoCatcher)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeClock lets the limiter wait without real time passing.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time        { return c.t }
func (c *fakeClock) sleep(d time.Duration) { c.t = c.t.Add(d) }

func TestLimiterKeepsTheRate(t *testing.T) {
	c := &fakeClock{t: time.Unix(1000, 0)}
	l := &limiter{now: c.now, sleep: c.sleep}
	l.SetRate(100 << 10) // 100 KiB/s
	start := c.t
	for i := 0; i < 64; i++ { // 1 MiB
		l.wait(context.Background(), relayChunk)
	}
	took := c.t.Sub(start).Seconds()
	if took < 9.5 || took > 10.5 {
		t.Fatalf("1 MiB at 100 KiB/s took %.2f s", took)
	}
	// A new limit counts at once.
	l.SetRate(1 << 20)
	start = c.t
	for i := 0; i < 64; i++ {
		l.wait(context.Background(), relayChunk)
	}
	if took := c.t.Sub(start).Seconds(); took > 1.1 {
		t.Fatalf("1 MiB at 1 MiB/s took %.2f s", took)
	}
	// No limit: no waiting.
	l.SetRate(0)
	start = c.t
	l.wait(context.Background(), 10<<20)
	if c.t != start {
		t.Fatal("waited without a limit")
	}
}

func TestMeterAveragesTheLastSeconds(t *testing.T) {
	m := &meter{}
	m.tick()
	for _, n := range []int{1000, 3000, 2000} {
		m.add(n)
		m.tick()
	}
	if got := m.tick(); got != 5000.0/3 {
		t.Fatalf("rate %v", got)
	}
}

func testRelay(t *testing.T) *relay {
	t.Helper()
	r, err := startRelay(newLimiter(), &meter{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	if !strings.HasPrefix(r.URL(), "http://vc:") || !strings.Contains(r.URL(), "@127.0.0.1:") {
		t.Fatalf("relay URL %s", r.URL())
	}
	return r
}

func clientVia(proxy string, tlsConf *tls.Config) *http.Client {
	u, _ := url.Parse(proxy) // ignore: the test made the string
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u), TLSClientConfig: tlsConf}, Timeout: 20 * time.Second}
}

// https goes through as a tunnel: the relay never sees inside, the client
// checks the site's certificate as without a relay.
func TestRelayTunnelsHTTPS(t *testing.T) {
	site := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(strings.Repeat("v", 200<<10))) // ignore: the test reads it
	}))
	defer site.Close()
	r := testRelay(t)
	resp, err := clientVia(r.URL(), site.Client().Transport.(*http.Transport).TLSClientConfig).Get(site.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body) // ignore: length checked
	resp.Body.Close()
	if len(b) != 200<<10 {
		t.Fatalf("got %d bytes", len(b))
	}
	if r.meter.total.Load() < 200<<10 {
		t.Fatalf("meter counted %d", r.meter.total.Load())
	}
}

// Plain http is forwarded without the proxy password and without
// X-Forwarded headers.
func TestRelayForwardsHTTPCleanly(t *testing.T) {
	var got http.Header
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got = req.Header.Clone()
		w.Write([]byte("hello")) // ignore: the test reads it
	}))
	defer site.Close()
	r := testRelay(t)
	resp, err := clientVia(r.URL(), nil).Get(site.URL + "/v.mp4")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body) // ignore: checked
	resp.Body.Close()
	if string(b) != "hello" {
		t.Fatalf("%q", b)
	}
	for _, h := range []string{"Proxy-Authorization", "X-Forwarded-For", "Forwarded"} {
		if got.Get(h) != "" {
			t.Errorf("site saw %s", h)
		}
	}
}

// Without the password the relay does nothing, also not for a web page in
// a browser that tries 127.0.0.1.
func TestRelayNeedsThePassword(t *testing.T) {
	hit := false
	site := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer site.Close()
	r := testRelay(t)
	u, _ := url.Parse(r.URL()) // ignore: made by the relay
	for _, proxy := range []string{"http://" + u.Host, "http://vc:wrong@" + u.Host} {
		resp, err := clientVia(proxy, nil).Get(site.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusProxyAuthRequired {
			t.Errorf("%s: status %d", proxy, resp.StatusCode)
		}
	}
	// A browser would send a normal request to the port, not a proxy one.
	req, _ := http.NewRequest(http.MethodGet, "http://"+u.Host+"/", nil) // ignore: constant URL
	req.Header.Set("Proxy-Authorization", r.auth)
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("plain request: status %d", resp.StatusCode)
	}
	if hit {
		t.Fatal("a request without the password reached the site")
	}
}

// The limit works on a real transfer: 256 KiB at 128 KiB/s take about two
// seconds, less what may pile up at the start.
func TestRelayLimitsARealTransfer(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(strings.Repeat("v", 256<<10))) // ignore: the test reads it
	}))
	defer site.Close()
	r := testRelay(t)
	r.lim.SetRate(128 << 10)
	start := time.Now()
	resp, err := clientVia(r.URL(), nil).Get(site.URL)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body) // ignore: only the time counts
	resp.Body.Close()
	if took := time.Since(start); took < 1300*time.Millisecond {
		t.Fatalf("256 KiB at 128 KiB/s took only %v", took)
	}
}

// 100 % is the fastest speed without a limit; until one was measured the
// slider cannot limit anything.
func TestSpeedControlPercentOfPeak(t *testing.T) {
	s := newSpeedControl(50, 0, slog.New(slog.DiscardHandler))
	defer s.Close()
	if s.ProxyURL() == "" || !s.State().Active {
		t.Fatal("no relay")
	}
	if s.lim.Rate() != 0 {
		t.Fatal("limit without a measured peak")
	}
	s.SetPercent(100)
	s.meter.add(4 << 20)
	s.Tick()
	s.meter.add(4 << 20)
	st, rose := s.Tick()
	if !rose || st.Peak < 2<<20 {
		t.Fatalf("peak not measured: %+v", st)
	}
	s.SetPercent(25)
	if want := st.Peak / 4; s.lim.Rate() != want {
		t.Fatalf("limit %v, want %v", s.lim.Rate(), want)
	}
	s.SetPercent(0)
	if s.State().Percent != 1 {
		t.Fatal("percent below 1 accepted")
	}
}

// A site that cannot be reached for a moment must not cost yt-dlp a retry.
func TestRelayDialsAgain(t *testing.T) {
	old := relayDialPauses
	relayDialPauses = []time.Duration{time.Millisecond, time.Millisecond}
	defer func() { relayDialPauses = old }()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	calls := 0
	r := &relay{dial: func(context.Context, string, string) (net.Conn, error) {
		if calls++; calls < 3 {
			return nil, errors.New("refused")
		}
		return a, nil
	}}
	c, err := r.dialPatiently(context.Background(), "example.com:443")
	if err != nil || c != a || calls != 3 {
		t.Fatalf("got %v, %v after %d calls", c, err, calls)
	}
	r.dial = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("down") }
	if _, err := r.dialPatiently(context.Background(), "example.com:443"); err == nil {
		t.Fatal("a site that stays down must fail")
	}
}
