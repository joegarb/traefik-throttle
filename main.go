package traefik_throttle

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Throttle is a Traefik middleware that caps the number of concurrent requests
// forwarded to a backend, queuing any excess instead of rejecting it outright.
type Throttle struct {
	config      *Config
	next        http.Handler
	name        string
	maxRequests int
	maxQueue    int
	maxWait     time.Duration
	verbose     bool
	retryAfter  string        // seconds, sent as the Retry-After header on 429s
	spacing     time.Duration // minimum gap between admissions to the backend

	mu     sync.Mutex
	active int             // slots currently reserved for the backend
	queue  []chan struct{} // FIFO of waiters; each is signalled when granted a slot

	paceMu    sync.Mutex
	lastAdmit time.Time // scheduled time of the previous admission
}

// Config holds the plugin configuration.
type Config struct {
	MaxRequests int    `json:"maxRequests"`
	MaxQueue    int    `json:"maxQueue"`
	MaxWait     string `json:"maxWait"`
	Verbose     bool   `json:"verbose"`
	Spacing     string `json:"spacing"`
}

// CreateConfig returns the default plugin configuration.
func CreateConfig() *Config {
	return &Config{
		MaxRequests: 10,
		MaxQueue:    100,
		MaxWait:     "5s",
		Verbose:     false,
		Spacing:     "20ms",
	}
}

// New builds a Throttle middleware from the given configuration.
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config == nil {
		config = CreateConfig()
	}

	if config.MaxRequests < 1 {
		config.MaxRequests = 1
	}
	if config.MaxQueue < 0 {
		config.MaxQueue = 0
	}
	maxWait, err := time.ParseDuration(config.MaxWait)
	if err != nil || maxWait < time.Millisecond {
		maxWait = time.Millisecond
		config.MaxWait = maxWait.String()
	}

	retryAfter := int64((maxWait + time.Second - 1) / time.Second) // ceil to seconds
	if retryAfter < 1 {
		retryAfter = 1
	}

	spacing, err := time.ParseDuration(config.Spacing)
	if err != nil || spacing < 0 {
		spacing = 0
		config.Spacing = "0s"
	}

	return &Throttle{
		config:      config,
		next:        next,
		name:        name,
		maxRequests: config.MaxRequests,
		maxQueue:    config.MaxQueue,
		maxWait:     maxWait,
		verbose:     config.Verbose,
		retryAfter:  strconv.FormatInt(retryAfter, 10),
		spacing:     spacing,
	}, nil
}

type acquireResult int

const (
	acquired acquireResult = iota
	queueFull
	waitTimeout
	clientGone
)

func (t *Throttle) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	switch t.acquire(req.Context()) {
	case acquired:
		defer t.release()
		t.pace()
		t.next.ServeHTTP(rw, req)
	case queueFull:
		t.logf("Request queue is full: %s\n", req.URL.String())
		t.reject(rw)
	case waitTimeout:
		t.logf("Timed out waiting: %s\n", req.URL.String())
		t.reject(rw)
	case clientGone:
		t.logf("Client disconnected: %s\n", req.URL.String())
	}
}

// reject responds with 429 and a Retry-After hint.
func (t *Throttle) reject(rw http.ResponseWriter) {
	rw.Header().Set("Retry-After", t.retryAfter)
	rw.WriteHeader(http.StatusTooManyRequests)
}

// logf writes a line only when verbose logging is enabled, so a busy queue
// doesn't flood the logs on the very servers this plugin is meant to protect.
func (t *Throttle) logf(format string, args ...interface{}) {
	if t.verbose {
		fmt.Printf(format, args...)
	}
}

// pace staggers admissions to the backend so a burst doesn't all land at once
// (which can overwhelm a slow upstream). It blocks until at least `spacing` has
// elapsed since the previous admission. No-op when spacing is 0.
func (t *Throttle) pace() {
	if t.spacing <= 0 {
		return
	}
	t.paceMu.Lock()
	next := t.lastAdmit.Add(t.spacing)
	if now := time.Now(); next.Before(now) {
		next = now
	}
	t.lastAdmit = next
	t.paceMu.Unlock()

	if wait := time.Until(next); wait > 0 {
		time.Sleep(wait)
	}
}

// acquire reserves a concurrency slot, blocking in FIFO order when all slots are
// busy. A newly arriving request only takes a slot immediately when no one is
// already waiting, so queued requests are never jumped by newcomers.
func (t *Throttle) acquire(ctx context.Context) acquireResult {
	t.mu.Lock()
	if t.active < t.maxRequests && len(t.queue) == 0 {
		t.active++
		t.mu.Unlock()
		return acquired
	}
	if len(t.queue) >= t.maxQueue {
		t.mu.Unlock()
		return queueFull
	}
	ch := make(chan struct{})
	t.queue = append(t.queue, ch)
	t.mu.Unlock()

	timer := time.NewTimer(t.maxWait)
	defer timer.Stop()

	select {
	case <-ch:
		return acquired
	case <-ctx.Done():
		t.giveUp(ch)
		return clientGone
	case <-timer.C:
		t.giveUp(ch)
		return waitTimeout
	}
}

// release returns a slot, handing it directly to the longest-waiting request if
// any are queued (FIFO); otherwise the reserved count drops.
func (t *Throttle) release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.queue) > 0 {
		ch := t.queue[0]
		t.queue = t.queue[1:]
		close(ch)
		return
	}
	t.active--
}

// giveUp is called by a waiter that stopped waiting. If it was still queued the
// reservation is untouched; if a slot had already been handed to it, that slot
// is passed on to the next waiter.
func (t *Throttle) giveUp(ch chan struct{}) {
	if !t.abandon(ch) {
		t.release()
	}
}

// abandon removes ch from the wait queue, returning true if it was still queued
// (false means a slot had already been granted to it).
func (t *Throttle) abandon(ch chan struct{}) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := 0; i < len(t.queue); i++ {
		if t.queue[i] == ch {
			t.queue = append(t.queue[:i], t.queue[i+1:]...)
			return true
		}
	}
	return false
}
