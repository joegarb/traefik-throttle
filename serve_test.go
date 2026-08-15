package traefik_throttle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingBackend is a fake upstream that lets a test control how many requests
// are concurrently "in flight". Each request records its arrival, then blocks
// until the test closes release.
type blockingBackend struct {
	arrived  chan struct{}
	release  chan struct{}
	inFlight int32
	maxSeen  int32
}

func newBlockingBackend() *blockingBackend {
	return &blockingBackend{
		arrived: make(chan struct{}, 128),
		release: make(chan struct{}),
	}
}

func (b *blockingBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := atomic.AddInt32(&b.inFlight, 1)
	for {
		m := atomic.LoadInt32(&b.maxSeen)
		if n <= m || atomic.CompareAndSwapInt32(&b.maxSeen, m, n) {
			break
		}
	}
	b.arrived <- struct{}{}
	<-b.release
	atomic.AddInt32(&b.inFlight, -1)
	w.WriteHeader(http.StatusOK)
}

func mustThrottle(t *testing.T, backend http.Handler, maxRequests, maxQueue int, maxWait string) http.Handler {
	t.Helper()
	h, err := New(context.Background(), backend, &Config{
		MaxRequests: maxRequests,
		MaxQueue:    maxQueue,
		MaxWait:     maxWait,
	}, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

// fireAsync sends one request through h in a goroutine, storing the status code.
func fireAsync(h http.Handler, ctx context.Context, wg *sync.WaitGroup, code *int32) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if ctx != nil {
			req = req.WithContext(ctx)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		atomic.StoreInt32(code, int32(rr.Code))
	}()
}

func waitWG(t *testing.T, wg *sync.WaitGroup, d time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("timed out waiting for requests to finish")
	}
}

func waitCode(t *testing.T, code *int32, d time.Duration) int32 {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if c := atomic.LoadInt32(code); c != 0 {
			return c
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("request did not return in time")
	return 0
}

// No more than maxRequests reach the backend at once; the rest queue and drain.
func TestConcurrencyLimitIsEnforced(t *testing.T) {
	b := newBlockingBackend()
	h := mustThrottle(t, b, 2, 10, "2s")

	var wg sync.WaitGroup
	codes := make([]int32, 5)
	for i := range codes {
		fireAsync(h, nil, &wg, &codes[i])
	}

	for i := 0; i < 2; i++ {
		select {
		case <-b.arrived:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d requests reached the backend, expected 2", i)
		}
	}
	select {
	case <-b.arrived:
		t.Fatal("a third request entered the backend while the limit was reached")
	case <-time.After(150 * time.Millisecond):
	}

	close(b.release)
	waitWG(t, &wg, 3*time.Second)

	if got := atomic.LoadInt32(&b.maxSeen); got != 2 {
		t.Errorf("max concurrent = %d, want 2", got)
	}
	for i := range codes {
		if c := atomic.LoadInt32(&codes[i]); c != http.StatusOK {
			t.Errorf("request %d: status %d, want 200", i, c)
		}
	}
}

// When the queue is full, the overflow request is rejected with 429.
func TestQueueFullReturns429(t *testing.T) {
	b := newBlockingBackend()
	h := mustThrottle(t, b, 1, 1, "2s")

	var wg sync.WaitGroup
	var aCode, bCode, cCode int32

	fireAsync(h, nil, &wg, &aCode) // takes the only slot, blocks in backend
	<-b.arrived
	fireAsync(h, nil, &wg, &bCode) // fills the single queue slot
	time.Sleep(100 * time.Millisecond)

	fireAsync(h, nil, &wg, &cCode) // queue full -> rejected
	if c := waitCode(t, &cCode, 2*time.Second); c != http.StatusTooManyRequests {
		t.Errorf("overflow request status = %d, want 429", c)
	}

	close(b.release)
	waitWG(t, &wg, 3*time.Second)
	if a, bb := atomic.LoadInt32(&aCode), atomic.LoadInt32(&bCode); a != http.StatusOK || bb != http.StatusOK {
		t.Errorf("A=%d B=%d, want 200/200", a, bb)
	}
}

// A queued request that waits longer than maxWait is rejected with 429.
func TestQueuedRequestTimesOut(t *testing.T) {
	b := newBlockingBackend()
	h := mustThrottle(t, b, 1, 10, "100ms")

	var wg sync.WaitGroup
	var aCode, bCode int32
	fireAsync(h, nil, &wg, &aCode)
	<-b.arrived
	start := time.Now()
	fireAsync(h, nil, &wg, &bCode)

	if c := waitCode(t, &bCode, 2*time.Second); c != http.StatusTooManyRequests {
		t.Errorf("timed-out request status = %d, want 429", c)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("request returned after %v, want >= maxWait (100ms)", elapsed)
	}
	select {
	case <-b.arrived:
		t.Fatal("timed-out request should not have reached the backend")
	default:
	}

	close(b.release)
	waitWG(t, &wg, 3*time.Second)
}

// A queued request is let through once a slot frees, never exceeding the limit.
func TestQueuedRequestEventuallyPasses(t *testing.T) {
	b := newBlockingBackend()
	h := mustThrottle(t, b, 1, 10, "2s")

	var wg sync.WaitGroup
	var aCode, bCode int32
	fireAsync(h, nil, &wg, &aCode)
	<-b.arrived
	fireAsync(h, nil, &wg, &bCode)
	time.Sleep(50 * time.Millisecond)
	close(b.release)
	waitWG(t, &wg, 3*time.Second)

	if a, bb := atomic.LoadInt32(&aCode), atomic.LoadInt32(&bCode); a != http.StatusOK || bb != http.StatusOK {
		t.Errorf("A=%d B=%d, want 200/200", a, bb)
	}
	if got := atomic.LoadInt32(&b.maxSeen); got != 1 {
		t.Errorf("max concurrent = %d, want 1", got)
	}
}

// If the client disconnects while queued, the request is abandoned promptly
// (well before maxWait) and never reaches the backend.
func TestClientCancelWhileQueued(t *testing.T) {
	b := newBlockingBackend()
	h := mustThrottle(t, b, 1, 10, "5s")

	var wg sync.WaitGroup
	var aCode, bCode int32
	fireAsync(h, nil, &wg, &aCode)
	<-b.arrived

	ctx, cancel := context.WithCancel(context.Background())
	start := time.Now()
	fireAsync(h, ctx, &wg, &bCode)
	time.Sleep(50 * time.Millisecond)
	cancel()

	waitCode(t, &bCode, 3*time.Second) // returns once ServeHTTP unwinds
	if elapsed := time.Since(start); elapsed >= 5*time.Second {
		t.Errorf("cancelled request took %v, expected prompt return", elapsed)
	}
	select {
	case <-b.arrived:
		t.Fatal("cancelled request should not have reached the backend")
	default:
	}

	close(b.release)
	waitWG(t, &wg, 3*time.Second)
}

// With spacing set, admissions to the backend are staggered rather than landing
// all at once — even when slots are freely available.
func TestSpacingStaggersAdmissions(t *testing.T) {
	const spacing = 40 * time.Millisecond

	var mu sync.Mutex
	var arrivals []time.Time
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		arrivals = append(arrivals, time.Now())
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	h, err := New(context.Background(), backend, &Config{
		MaxRequests: 100, MaxQueue: 100, MaxWait: "5s", Spacing: spacing.String(),
	}, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		}()
	}
	waitWG(t, &wg, 3*time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(arrivals) != 4 {
		t.Fatalf("got %d admissions, want 4", len(arrivals))
	}
	sort.Slice(arrivals, func(i, j int) bool { return arrivals[i].Before(arrivals[j]) })
	for i := 1; i < len(arrivals); i++ {
		if gap := arrivals[i].Sub(arrivals[i-1]); gap < spacing/2 {
			t.Errorf("admissions %d→%d only %v apart, want ~%v", i-1, i, gap, spacing)
		}
	}
}

func firePath(h http.Handler, path string, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}()
}

// Queued requests are granted slots in the order they arrived.
func TestQueuedRequestsServedInFIFOOrder(t *testing.T) {
	firstIn := make(chan struct{})
	releaseFirst := make(chan struct{})
	var mu sync.Mutex
	var order []string

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		order = append(order, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/first" {
			close(firstIn)
			<-releaseFirst
		}
		w.WriteHeader(http.StatusOK)
	})

	h := mustThrottle(t, backend, 1, 10, "5s")

	var wg sync.WaitGroup
	firePath(h, "/first", &wg) // takes the only slot, blocks
	<-firstIn
	for _, p := range []string{"/b", "/c", "/d"} {
		firePath(h, p, &wg)
		time.Sleep(40 * time.Millisecond) // ensure each enqueues in order
	}
	close(releaseFirst)
	waitWG(t, &wg, 3*time.Second)

	mu.Lock()
	got := strings.Join(order, ",")
	mu.Unlock()
	if got != "/first,/b,/c,/d" {
		t.Errorf("service order = %q, want /first,/b,/c,/d", got)
	}
}

// Rejections carry a Retry-After header derived from maxWait.
func TestRejectionSetsRetryAfter(t *testing.T) {
	b := newBlockingBackend()
	h := mustThrottle(t, b, 1, 0, "5s") // maxQueue 0 -> reject immediately when busy

	var wg sync.WaitGroup
	var aCode int32
	fireAsync(h, nil, &wg, &aCode)
	<-b.arrived // A holds the only slot

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req) // rejected synchronously
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rr.Code)
	}
	if ra := rr.Header().Get("Retry-After"); ra != "5" {
		t.Errorf("Retry-After = %q, want \"5\"", ra)
	}

	close(b.release)
	waitWG(t, &wg, 3*time.Second)
}

// Under heavy contention with some clients cancelling, the limit is never
// breached, no slots leak, and every request terminates (guards the handoff
// and abandon paths; most valuable with -race).
func TestStressStaysWithinLimit(t *testing.T) {
	const limit = 4
	var inFlight, maxSeen int32

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			m := atomic.LoadInt32(&maxSeen)
			if n <= m || atomic.CompareAndSwapInt32(&maxSeen, m, n) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		w.WriteHeader(http.StatusOK)
	})
	h := mustThrottle(t, backend, limit, 1000, "2s")

	var wg sync.WaitGroup
	for i := 0; i < 300; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := context.Background()
			if i%5 == 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), time.Duration(i%3)*time.Millisecond)
				defer cancel()
			}
			req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
			h.ServeHTTP(httptest.NewRecorder(), req)
		}(i)
	}
	waitWG(t, &wg, 15*time.Second)

	if got := atomic.LoadInt32(&maxSeen); got > limit {
		t.Errorf("max concurrent = %d, exceeded limit %d", got, limit)
	}
	if got := atomic.LoadInt32(&inFlight); got != 0 {
		t.Errorf("inFlight = %d after drain, want 0 (leaked slot)", got)
	}
}

// nopResponseWriter discards everything, so benchmarks measure the middleware
// rather than response recording.
type nopResponseWriter struct{ header http.Header }

func (w nopResponseWriter) Header() http.Header         { return w.header }
func (w nopResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w nopResponseWriter) WriteHeader(int)             {}

// Overhead of the middleware on the fast path (a slot is always free).
func BenchmarkServeHTTP(b *testing.B) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	h, err := New(context.Background(), next, &Config{MaxRequests: 1000, MaxQueue: 1000, MaxWait: "5s"}, "bench")
	if err != nil {
		b.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := nopResponseWriter{header: make(http.Header)}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.ServeHTTP(rw, req)
	}
}

// Throughput under concurrency, with enough slots that requests rarely queue.
func BenchmarkServeHTTPParallel(b *testing.B) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	h, err := New(context.Background(), next, &Config{MaxRequests: 100, MaxQueue: 100000, MaxWait: "5s"}, "bench")
	if err != nil {
		b.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		rw := nopResponseWriter{header: make(http.Header)}
		for pb.Next() {
			h.ServeHTTP(rw, req)
		}
	})
}
