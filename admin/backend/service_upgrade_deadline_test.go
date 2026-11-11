package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type delayedUpgradeResponse struct{ http.ResponseWriter }

func (w delayedUpgradeResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w delayedUpgradeResponse) WriteHeader(status int) {
	time.Sleep(100 * time.Millisecond)
	w.ResponseWriter.WriteHeader(status)
}

func TestUpgradeResponseSurvivesNormalServerWriteDeadline(t *testing.T) {
	s := &Server{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.servicesUpgradeAPI(delayedUpgradeResponse{w}, r)
	}))
	server.Config.WriteTimeout = 20 * time.Millisecond
	server.Start()
	defer server.Close()
	client := server.Client()
	client.Timeout = 2 * time.Second
	response, err := client.Post(server.URL, "application/json", strings.NewReader(`{"service":"unsupported-test-service"}`))
	if err != nil {
		t.Fatalf("upgrade response lost to normal write deadline: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "error") {
		t.Fatalf("unexpected upgrade response: status=%d body=%s err=%v", response.StatusCode, body, err)
	}
}
