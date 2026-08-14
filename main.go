package traefik_throttle

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const typeName = "Throttle"

type Throttle struct {
	config      *Config
	next        http.Handler
	name        string
	maxRequests int
	maxQueue    int
	maxWait     time.Duration
	verbose     bool
	retryAfter  string // seconds, sent as the Retry-After header on 429s

	mu     sync.Mutex
	active int             // slots currently reserved for the backend
	queue  []chan struct{} // FIFO of waiters; each is signalled when granted a slot
}

type Config struct {
	MaxRequests int    `json:"maxRequests"`
	MaxQueue    int    `json:"maxQueue"`
	MaxWait     string `json:"maxWait"`
	Verbose     bool   `json:"verbose"`
}

func CreateConfig() *Config {
	return &Config{
		MaxRequests: 10,
		MaxQueue:    100,
		MaxWait:     "5s",
		Verbose:     false,
	}
}

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

	return &Throttle{
		config:      config,
		next:        next,
		name:        name,
		maxRequests: config.MaxRequests,
		maxQueue:    config.MaxQueue,
		maxWait:     maxWait,
		verbose:     config.Verbose,
		retryAfter:  strconv.FormatInt(retryAfter, 10),
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
	for i, w := range t.queue {
		if w == ch {
			t.queue = append(t.queue[:i], t.queue[i+1:]...)
			return true
		}
	}
	return false
}
