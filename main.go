package traefik_throttle

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const typeName = "Throttle"

type Throttle struct {
	config   *Config
	next     http.Handler
	name     string
	sem      chan struct{}
	maxQueue int
	maxWait  time.Duration
	mu       sync.Mutex
	waiting  int
}

type Config struct {
	MaxRequests int    `json:"maxRequests"`
	MaxQueue    int    `json:"maxQueue"`
	MaxWait     string `json:"maxWait"`
}

func CreateConfig() *Config {
	return &Config{
		MaxRequests: 10,
		MaxQueue:    100,
		MaxWait:     "5s",
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

	return &Throttle{
		config:   config,
		next:     next,
		name:     name,
		sem:      make(chan struct{}, config.MaxRequests),
		maxQueue: config.MaxQueue,
		maxWait:  maxWait,
	}, nil
}

func (t *Throttle) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	select {
	case t.sem <- struct{}{}:
		defer func() { <-t.sem }()
		t.next.ServeHTTP(rw, req)
		return
	default:
	}

	t.mu.Lock()
	if t.waiting >= t.maxQueue {
		t.mu.Unlock()
		fmt.Printf("Request queue is full: %s\n", req.URL.String())
		rw.WriteHeader(http.StatusTooManyRequests)
		return
	}
	t.waiting++
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		t.waiting--
		t.mu.Unlock()
	}()

	fmt.Printf("Queuing request (max wait %s): %s\n", t.maxWait, req.URL.String())

	select {
	case t.sem <- struct{}{}:
		defer func() { <-t.sem }()
		fmt.Printf("Passing queued request: %s\n", req.URL.String())
		t.next.ServeHTTP(rw, req)
	case <-req.Context().Done():
		fmt.Printf("Client disconnected: %s\n", req.URL.String())
	case <-time.After(t.maxWait):
		fmt.Printf("Timed out waiting: %s\n", req.URL.String())
		rw.WriteHeader(http.StatusTooManyRequests)
	}
}
