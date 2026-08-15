// Package yaegicheck lives in its own module so the Yaegi dependency it needs
// to exercise Traefik's interpreter never enters the plugin's own go.mod (the
// Traefik Plugin Catalog rejects plugins that depend on github.com/traefik/yaegi).
package yaegicheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// pluginSource reads the plugin's source from the parent module so Yaegi can
// interpret it (as Traefik does).
func pluginSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../main.go")
	if err != nil {
		t.Fatalf("read plugin source: %v", err)
	}
	return string(b)
}

// interpretedHandler loads the plugin through Yaegi and returns its middleware
// handler, so tests exercise the real interpreted code paths — not the natively
// compiled ones.
func interpretedHandler(t *testing.T, next http.Handler, maxRequests, maxQueue int, maxWait string) http.Handler {
	t.Helper()

	i := interp.New(interp.Options{})
	if err := i.Use(stdlib.Symbols); err != nil {
		t.Fatalf("use stdlib: %v", err)
	}
	if _, err := i.Eval(pluginSource(t)); err != nil {
		t.Fatalf("plugin is not Yaegi-compatible: %v", err)
	}

	createConfig, err := i.Eval("traefik_throttle.CreateConfig")
	if err != nil {
		t.Fatalf("eval CreateConfig: %v", err)
	}
	newFn, err := i.Eval("traefik_throttle.New")
	if err != nil {
		t.Fatalf("eval New: %v", err)
	}

	cfg := createConfig.Call(nil)[0]
	elem := cfg.Elem()
	elem.FieldByName("MaxRequests").SetInt(int64(maxRequests))
	elem.FieldByName("MaxQueue").SetInt(int64(maxQueue))
	elem.FieldByName("MaxWait").SetString(maxWait)

	out := newFn.Call([]reflect.Value{
		reflect.ValueOf(context.Background()),
		reflect.ValueOf(next),
		cfg,
		reflect.ValueOf("test"),
	})
	if !out[1].IsNil() {
		t.Fatalf("New returned error: %v", out[1].Interface())
	}
	h, ok := out[0].Interface().(http.Handler)
	if !ok {
		t.Fatalf("interpreted New did not return an http.Handler")
	}
	return h
}

// Interpreting the plugin source catches Yaegi-incompatible syntax.
func TestYaegiCanInterpretPlugin(t *testing.T) {
	i := interp.New(interp.Options{})
	if err := i.Use(stdlib.Symbols); err != nil {
		t.Fatalf("use stdlib: %v", err)
	}
	if _, err := i.Eval(pluginSource(t)); err != nil {
		t.Fatalf("plugin is not Yaegi-compatible: %v", err)
	}
}

// Driving requests through the interpreter catches constructs that interpret
// fine but panic at runtime (e.g. the queue/abandon path under old Yaegi).
func TestYaegiRuntimePaths(t *testing.T) {
	blocked := make(chan struct{})
	var reached int32
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		atomic.AddInt32(&reached, 1)
		<-blocked
	})

	h := interpretedHandler(t, next, 1, 10, "80ms")

	serve := func(ctx context.Context) int {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("plugin panicked under Yaegi: %v", r)
			}
		}()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if ctx != nil {
			req = req.WithContext(ctx)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}

	// Occupy the single slot with a request that blocks in the backend.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); serve(context.Background()) }()
	for atomic.LoadInt32(&reached) == 0 {
		time.Sleep(2 * time.Millisecond)
	}

	// Queue a request that waits past maxWait -> exercises abandon().
	if code := serve(context.Background()); code != http.StatusTooManyRequests {
		t.Errorf("queued+timed-out request: got %d, want 429", code)
	}

	// Queue a request whose client cancels -> also exercises abandon().
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	serve(ctx)

	// Release the slot holder; the freed slot passes through release().
	close(blocked)
	wg.Wait()
}
