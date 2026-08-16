package traefik_throttle

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
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

	// sem is a counting semaphore: it holds one token per in-flight request and
	// is buffered to maxRequests, so its capacity — not hand-maintained
	// bookkeeping — is what caps concurrency. Because a send either completes
	// (slot taken) or doesn't (lost the select to a timeout/cancel), there is no
	// "granted but not yet consumed" window, so the limit holds under any
	// goroutine scheduling, including Traefik's Yaegi interpreter.
	sem     chan struct{}
	waiting int32 // requests currently queued for a slot, for the maxQueue bound

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
		sem:         make(chan struct{}, config.MaxRequests),
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

// acquire reserves a concurrency slot. A free slot is taken immediately;
// otherwise the request queues (FIFO, up to maxQueue) until a slot frees, the
// wait exceeds maxWait, or the client goes away.
func (t *Throttle) acquire(ctx context.Context) acquireResult {
	// Fast path: take a free slot without blocking. When every slot is busy this
	// send fails and we fall through to the queue, so a newcomer never jumps an
	// already-waiting request — Go serves the channel's blocked senders in FIFO
	// order, and while any are waiting the buffer is full (this send can't win).
	select {
	case t.sem <- struct{}{}:
		return acquired
	default:
	}

	// Bound the queue before parking.
	if int(atomic.AddInt32(&t.waiting, 1)) > t.maxQueue {
		atomic.AddInt32(&t.waiting, -1)
		return queueFull
	}
	defer atomic.AddInt32(&t.waiting, -1)

	timer := time.NewTimer(t.maxWait)
	defer timer.Stop()

	// Exactly one case fires: either the send completes (we hold a slot) or we
	// bail with none, so a slot is never reserved and then abandoned.
	select {
	case t.sem <- struct{}{}:
		return acquired
	case <-ctx.Done():
		return clientGone
	case <-timer.C:
		return waitTimeout
	}
}

// release frees the slot held by an in-flight request; a queued request (if any)
// is woken to take it, in arrival order.
func (t *Throttle) release() {
	<-t.sem
}
