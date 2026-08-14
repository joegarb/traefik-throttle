package traefik_throttle

import (
	"context"
	"testing"
	"time"
)

func TestNewWithZeroMaxRequests(t *testing.T) {
	config := &Config{
		MaxRequests: 0,
		MaxQueue:    100,
		MaxWait:     "5s",
	}

	handler, err := New(context.Background(), nil, config, "")
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
		t.Errorf("Expected maxRequests to be 1, got %d", throttle.maxRequests)
	}
}

func TestNewWithPositiveMaxRequests(t *testing.T) {
	config := &Config{
		MaxRequests: 10,
		MaxQueue:    100,
		MaxWait:     "5s",
	}

	handler, err := New(context.Background(), nil, config, "")
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
		t.Errorf("Expected maxRequests to be 10, got %d", throttle.maxRequests)
	}
}

func TestNewWithNegativeMaxQueue(t *testing.T) {
	config := &Config{
		MaxRequests: 100,
		MaxQueue:    -10,
		MaxWait:     "5s",
	}

	handler, err := New(context.Background(), nil, config, "")
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
		MaxWait:     "5s",
	}

	handler, err := New(context.Background(), nil, config, "")
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

func TestNewWithInvalidMaxWait(t *testing.T) {
	config := &Config{
		MaxRequests: 100,
		MaxQueue:    100,
		MaxWait:     "foo",
	}

	handler, err := New(context.Background(), nil, config, "")
	if err != nil {
		t.Errorf("Error creating Throttle: %v", err)
	}

	throttle, ok := handler.(*Throttle)
	if !ok {
		t.Error("Invalid handler type")
	}

	if throttle.config.MaxWait != "1ms" {
		t.Errorf("Expected config.MaxWait to be 1ms, got %s", throttle.config.MaxWait)
	}
	if throttle.maxWait != time.Millisecond {
		t.Errorf("Expected throttle.maxWait to be 1ms, got %s", throttle.maxWait)
	}
}

func TestNewWithZeroMaxWait(t *testing.T) {
	config := &Config{
		MaxRequests: 100,
		MaxQueue:    100,
		MaxWait:     "0ms",
	}

	handler, err := New(context.Background(), nil, config, "")
	if err != nil {
		t.Errorf("Error creating Throttle: %v", err)
	}

	throttle, ok := handler.(*Throttle)
	if !ok {
		t.Error("Invalid handler type")
	}

	if throttle.config.MaxWait != "1ms" {
		t.Errorf("Expected config.MaxWait to be 1ms, got %s", throttle.config.MaxWait)
	}
	if throttle.maxWait != time.Millisecond {
		t.Errorf("Expected throttle.maxWait to be 1ms, got %s", throttle.maxWait)
	}
}

func TestNewWithPositiveMaxWait(t *testing.T) {
	config := &Config{
		MaxRequests: 100,
		MaxQueue:    100,
		MaxWait:     "5s",
	}

	handler, err := New(context.Background(), nil, config, "")
	if err != nil {
		t.Errorf("Error creating Throttle: %v", err)
	}

	throttle, ok := handler.(*Throttle)
	if !ok {
		t.Error("Invalid handler type")
	}

	if throttle.config.MaxWait != "5s" {
		t.Errorf("Expected config.MaxWait to be 5s, got %s", throttle.config.MaxWait)
	}
	if throttle.maxWait != 5*time.Second {
		t.Errorf("Expected throttle.maxWait to be 5s, got %s", throttle.maxWait)
	}
}
