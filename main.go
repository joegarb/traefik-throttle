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
	retryCount  int
	retryDelay  time.Duration

	requestsCount int
	mutex         sync.Mutex
}

type Config struct {
	MaxRequests int    `json:"maxRequests"`
	RetryCount  int    `json:"retryCount"`
	RetryDelay  string `json:"retryDelay"`
}

func CreateConfig() *Config {
	return &Config{
		MaxRequests: 100,
		RetryCount:  3,
		RetryDelay:  "200ms",
	}
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config == nil {
		config = &Config{
			MaxRequests: 100,
			RetryCount:  3,
			RetryDelay:  "200ms",
		}
	}

	retryDelay, err := time.ParseDuration(config.RetryDelay)
	if err != nil {
		return nil, fmt.Errorf("failed to parse RetryDelay: %w", err)
	}

	return &Throttle{
		config:      config,
		next:        next,
		name:        name,
		maxRequests: config.MaxRequests,
		retryCount:  config.RetryCount,
		retryDelay:  retryDelay,
	}, nil
}

func (t *Throttle) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	retryCount := t.retryCount

	for retryCount >= 0 {
		t.mutex.Lock()
		if t.requestsCount < t.maxRequests {
			t.requestsCount++
			if retryCount < t.retryCount {
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

		fmt.Printf("Too many requests; will retry %d time(s): %s\n", retryCount, req.URL.String())
		retryCount--
		time.Sleep(t.retryDelay)
	}

	fmt.Printf("Exhausted retry limit: %s\n", req.URL.String())
	rw.WriteHeader(http.StatusTooManyRequests)
}
