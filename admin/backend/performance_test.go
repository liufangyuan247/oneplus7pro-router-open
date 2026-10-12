package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCachedSnapshotCoalescesConcurrentClients(t *testing.T) {
	var calls atomic.Int32
	s := &Server{
		collectSnapshot: func() (Snapshot, error) {
			calls.Add(1)
			time.Sleep(20 * time.Millisecond)
			return Snapshot{Time: "shared"}, nil
		},
	}

	const clients = 24
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, clients)
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			snapshot, err := s.cachedSnapshot(context.Background(), false)
			if err == nil && snapshot.Time != "shared" {
				t.Errorf("unexpected snapshot: %#v", snapshot)
			}
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("collector ran %d times for %d concurrent clients, want 1", got, clients)
	}

	if _, err := s.cachedSnapshot(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("warm cache ran collector again: %d", got)
	}
	if _, err := s.cachedSnapshot(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("forced refresh collector calls = %d, want 2", got)
	}
}

func TestMihomoConnectionsCoalescesConsumers(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/connections" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"downloadTotal":12,"uploadTotal":34,"connections":[{"id":"one","upload":1,"download":2,"metadata":{"sourceIP":"192.168.2.2"}}]}`)
	}))
	defer upstream.Close()

	s := &Server{mihomoController: upstream.URL}
	const consumers = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < consumers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			response, err := s.getMihomoConnections(context.Background(), time.Second)
			if err != nil {
				t.Errorf("connections request failed: %v", err)
				return
			}
			if len(response.Connections) != 1 || response.DownloadTotal != 12 {
				t.Errorf("unexpected response: %#v", response)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("upstream ran %d times for %d consumers, want 1", got, consumers)
	}
}

func TestMetricHistoryRetainsCompleteTwoHourWindow(t *testing.T) {
	s := &Server{}
	base := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 1500; i++ {
		s.appendMetricPoint(MetricHistoryPoint{
			At:  base.Add(time.Duration(i) * metricSampleInterval).UnixMilli(),
			CPU: float64(i),
		})
	}

	history := s.metricHistorySnapshot()
	if len(history) != 1441 {
		t.Fatalf("history contains %d points, want 1441 inclusive five-second samples", len(history))
	}
	if span := time.Duration(history[len(history)-1].At-history[0].At) * time.Millisecond; span != metricHistoryWindow {
		t.Fatalf("history span = %v, want %v", span, metricHistoryWindow)
	}

	// Callers receive their own slice and cannot corrupt the server ring.
	history[0].CPU = -1
	if got := s.metricHistorySnapshot()[0].CPU; got == -1 {
		t.Fatal("history snapshot aliases the server ring")
	}
}
