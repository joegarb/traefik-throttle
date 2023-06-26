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
	config *Config
	next   http.Handler
	name   string

	maxRequests int
	maxQueue    int
	retryCount  int
	retryDelay  time.Duration

	requestsCount int
	queueCount    int
	mutex         sync.Mutex
}

type Config struct {
	MaxRequests int    `json:"maxRequests"`
	MaxQueue    int    `json:"maxQueue"`
	RetryCount  int    `json:"retryCount"`
	RetryDelay  string `json:"retryDelay"`
}

func CreateConfig() *Config {
	return &Config{
		MaxRequests: 100,
		MaxQueue:    100,
		RetryCount:  3,
		RetryDelay:  "200ms",
	}
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config == nil {
		config = &Config{
			MaxRequests: 100,
			MaxQueue:    100,
			RetryCount:  3,
			RetryDelay:  "200ms",
		}
	}

	retryDelay, err := time.ParseDuration(config.RetryDelay)
	if err != nil {
		return nil, fmt.Errorf("failed to parse RetryDelay: %w", err)
	}

	return &Throttle{
		config:        config,
		next:          next,
		name:          name,
		maxRequests:   config.MaxRequests,
		maxQueue:      config.MaxQueue,
		retryCount:    config.RetryCount,
		retryDelay:    retryDelay,
		requestsCount: 0,
		queueCount:    0,
	}, nil
}

func (t *Throttle) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	attempt := t.retryCount
	queued := false

	for attempt >= 0 {
		t.mutex.Lock()
		if t.requestsCount < t.maxRequests {
			t.requestsCount++
			if queued {
				queued = false
				t.queueCount--
				fmt.Printf("Passing request after retry: %s\n", req.URL.String())
			}

			t.mutex.Unlock()
			defer func() {
				t.mutex.Lock()
				t.requestsCount-- // Decrement requestsCount after the response is received
				t.mutex.Unlock()
			}()

			t.next.ServeHTTP(rw, req) // Pass the request to the next middleware
			return
		}
		t.mutex.Unlock()

		if !queued {
			t.mutex.Lock()
			if t.queueCount >= t.maxQueue {
				t.mutex.Unlock()
				fmt.Printf("Request queue is full: %s\n", req.URL.String())
				rw.WriteHeader(http.StatusTooManyRequests)
				return
			}
			t.queueCount++
			queued = true
			t.mutex.Unlock()
		}

		fmt.Printf("Too many requests; will retry %d time(s): %s\n", attempt, req.URL.String())
		attempt--
		time.Sleep(t.retryDelay)
	}

	if queued {
		queued = false
		t.mutex.Lock()
		t.queueCount--
		t.mutex.Unlock()
	}

	fmt.Printf("Exhausted retry limit: %s\n", req.URL.String())
	rw.WriteHeader(http.StatusTooManyRequests)
}
