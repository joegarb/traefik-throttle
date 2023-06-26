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
	t.next.ServeHTTP(rw, req)
}
