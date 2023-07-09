package traefik_throttle

import (
	"testing"
	"time"
)

func TestNewWithZeroMaxRequests(t *testing.T) {
	config := &Config{
		MaxRequests: 0,
		MaxQueue:    100,
		RetryCount:  3,
		RetryDelay:  "200ms",
	}

	handler, err := New(nil, nil, config, "")
	if err != nil {
		t.Errorf("Error creating Throttle: %v", err)
	}
	throttle, ok := handler.(*Throttle)
	if !ok {
		t.Error("Invalid handler type")
	}
	if throttle.config.MaxRequests != 1 {
		t.Errorf("Expected config.MaxRequests to be 1, got %d", throttle.config.MaxRequests)
	}
	if throttle.maxRequests != 1 {
		t.Errorf("Expected throttle.maxRequests to be 1, got %d", throttle.maxRequests)
	}
}

func TestNewWithPositiveMaxRequests(t *testing.T) {
	config := &Config{
		MaxRequests: 10,
		MaxQueue:    100,
		RetryCount:  3,
		RetryDelay:  "200ms",
	}

	handler, err := New(nil, nil, config, "")
	if err != nil {
		t.Errorf("Error creating Throttle: %v", err)
	}
	throttle, ok := handler.(*Throttle)
	if !ok {
		t.Error("Invalid handler type")
	}
	if throttle.config.MaxRequests != 10 {
		t.Errorf("Expected config.MaxRequests to be 10, got %d", throttle.config.MaxRequests)
	}
	if throttle.maxRequests != 10 {
		t.Errorf("Expected throttle.maxRequests to be 10, got %d", throttle.maxRequests)
	}
}

func TestNewWithNegativeMaxQueue(t *testing.T) {
	config := &Config{
		MaxRequests: 100,
		MaxQueue:    -10,
		RetryCount:  3,
		RetryDelay:  "200ms",
	}

	handler, err := New(nil, nil, config, "")
	if err != nil {
		t.Errorf("Error creating Throttle: %v", err)
	}
	throttle, ok := handler.(*Throttle)
	if !ok {
		t.Error("Invalid handler type")
	}
	if throttle.config.MaxQueue != 0 {
		t.Errorf("Expected config.MaxQueue to be 0, got %d", throttle.config.MaxQueue)
	}
	if throttle.maxQueue != 0 {
		t.Errorf("Expected throttle.maxQueue to be 0, got %d", throttle.maxQueue)
	}
}

func TestNewWithPositiveMaxQueue(t *testing.T) {
	config := &Config{
		MaxRequests: 100,
		MaxQueue:    10,
		RetryCount:  3,
		RetryDelay:  "200ms",
	}

	handler, err := New(nil, nil, config, "")
	if err != nil {
		t.Errorf("Error creating Throttle: %v", err)
	}
	throttle, ok := handler.(*Throttle)
	if !ok {
		t.Error("Invalid handler type")
	}
	if throttle.config.MaxQueue != 10 {
		t.Errorf("Expected config.MaxQueue to be 10, got %d", throttle.config.MaxQueue)
	}
	if throttle.maxQueue != 10 {
		t.Errorf("Expected throttle.maxQueue to be 10, got %d", throttle.maxQueue)
	}
}

func TestNewWithNegativeRetryCount(t *testing.T) {
	config := &Config{
		MaxRequests: 100,
		MaxQueue:    100,
		RetryCount:  -5,
		RetryDelay:  "200ms",
	}

	handler, err := New(nil, nil, config, "")
	if err != nil {
		t.Errorf("Error creating Throttle: %v", err)
	}

	throttle, ok := handler.(*Throttle)
	if !ok {
		t.Error("Invalid handler type")
	}

	if throttle.config.RetryCount != 0 {
		t.Errorf("Expected config.RetryCount to be 0, got %d", throttle.config.RetryCount)
	}
	if throttle.retryCount != 0 {
		t.Errorf("Expected throttle.retryCount to be 0, got %d", throttle.retryCount)
	}
}

func TestNewWithPositiveRetryCount(t *testing.T) {
	config := &Config{
		MaxRequests: 100,
		MaxQueue:    100,
		RetryCount:  5,
		RetryDelay:  "200ms",
	}

	handler, err := New(nil, nil, config, "")
	if err != nil {
		t.Errorf("Error creating Throttle: %v", err)
	}

	throttle, ok := handler.(*Throttle)
	if !ok {
		t.Error("Invalid handler type")
	}

	if throttle.config.RetryCount != 5 {
		t.Errorf("Expected config.RetryCount to be 5, got %d", throttle.config.RetryCount)
	}
	if throttle.retryCount != 5 {
		t.Errorf("Expected throttle.retryCount to be 5, got %d", throttle.retryCount)
	}
}

func TestNewWithInvalidRetryDelay(t *testing.T) {
	config := &Config{
		MaxRequests: 100,
		MaxQueue:    100,
		RetryCount:  3,
		RetryDelay:  "foo",
	}

	handler, err := New(nil, nil, config, "")
	if err != nil {
		t.Errorf("Error creating Throttle: %v", err)
	}

	throttle, ok := handler.(*Throttle)
	if !ok {
		t.Error("Invalid handler type")
	}

	expectedRetryDelay := time.Millisecond
	if throttle.config.RetryDelay != "1ms" {
		t.Errorf("Expected config.RetryDelay to be %s, got %s", expectedRetryDelay, throttle.config.RetryDelay)
	}
	if throttle.retryDelay != expectedRetryDelay {
		t.Errorf("Expected throttle.retryDelay to be %s, got %s", expectedRetryDelay, throttle.retryDelay)
	}
}

func TestNewWithZeroRetryDelay(t *testing.T) {
	config := &Config{
		MaxRequests: 100,
		MaxQueue:    100,
		RetryCount:  3,
		RetryDelay:  "0ms",
	}

	handler, err := New(nil, nil, config, "")
	if err != nil {
		t.Errorf("Error creating Throttle: %v", err)
	}

	throttle, ok := handler.(*Throttle)
	if !ok {
		t.Error("Invalid handler type")
	}

	expectedRetryDelay := time.Millisecond
	if throttle.config.RetryDelay != "1ms" {
		t.Errorf("Expected config.RetryDelay to be %s, got %s", expectedRetryDelay, throttle.config.RetryDelay)
	}
	if throttle.retryDelay != expectedRetryDelay {
		t.Errorf("Expected throttle.retryDelay to be %s, got %s", expectedRetryDelay, throttle.retryDelay)
	}
}

func TestNewWithPositiveRetryDelay(t *testing.T) {
	config := &Config{
		MaxRequests: 100,
		MaxQueue:    100,
		RetryCount:  3,
		RetryDelay:  "100ms",
	}

	handler, err := New(nil, nil, config, "")
	if err != nil {
		t.Errorf("Error creating Throttle: %v", err)
	}

	throttle, ok := handler.(*Throttle)
	if !ok {
		t.Error("Invalid handler type")
	}

	expectedRetryDelay := 100 * time.Millisecond
	if throttle.config.RetryDelay != "100ms" {
		t.Errorf("Expected config.RetryDelay to be %s, got %s", expectedRetryDelay, throttle.config.RetryDelay)
	}
	if throttle.retryDelay != expectedRetryDelay {
		t.Errorf("Expected throttle.retryDelay to be %s, got %s", expectedRetryDelay, throttle.retryDelay)
	}
}
