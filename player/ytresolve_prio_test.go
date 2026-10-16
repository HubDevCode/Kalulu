package player

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// A click on a page whose prefetch still waits in the low-priority queue
// resolves at its own priority instead of waiting behind it.
func TestResolveYTClickDoesNotWaitForPrefetch(t *testing.T) {
	const page = "https://www.youtube.com/watch?v=prioTest001"
	release := make(chan struct{})
	var calls atomic.Int32
	old := ytRun
	ytRun = func(ctx context.Context, pageURL string, prio int) (YTResolved, error) {
		calls.Add(1)
		if prio == 1 {
			<-release // the prefetch is stuck behind others
		}
		return YTResolved{URL: "https://example.invalid/audio", expires: time.Now().Add(time.Hour)}, nil
	}
	t.Cleanup(func() {
		ytRun = old
		ForgetYT(page)
	})

	prefetched := make(chan struct{})
	go func() {
		_, _ = ResolveYTBackground(context.Background(), page)
		close(prefetched)
	}()
	waitFor(t, func() bool { return calls.Load() == 1 })

	done := make(chan error, 1)
	go func() {
		_, err := ResolveYT(context.Background(), page)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ResolveYT: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the click waited for the queued prefetch")
	}
	close(release)
	<-prefetched
}

// A click that finds the prefetch finished first uses its answer.
func TestResolveYTClickTakesFinishedPrefetch(t *testing.T) {
	const page = "https://www.youtube.com/watch?v=prioTest002"
	release := make(chan struct{})
	old := ytRun
	ytRun = func(ctx context.Context, pageURL string, prio int) (YTResolved, error) {
		if prio == 0 {
			<-release // the click's own request is slow
			return YTResolved{URL: "https://example.invalid/own", expires: time.Now().Add(time.Hour)}, nil
		}
		time.Sleep(50 * time.Millisecond)
		return YTResolved{URL: "https://example.invalid/prefetch", expires: time.Now().Add(time.Hour)}, nil
	}
	t.Cleanup(func() {
		close(release)
		ytRun = old
		ForgetYT(page)
	})

	go func() { _, _ = ResolveYTBackground(context.Background(), page) }()
	time.Sleep(10 * time.Millisecond)
	res, err := ResolveYT(context.Background(), page)
	if err != nil || res.URL != "https://example.invalid/prefetch" {
		t.Fatalf("ResolveYT = %q, %v; want the prefetch answer", res.URL, err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
