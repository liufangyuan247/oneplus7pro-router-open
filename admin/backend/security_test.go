package main

import (
	"net/http/httptest"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		host   string
		want   bool
	}{
		{name: "no origin", host: "192.168.2.1:9090", want: true},
		{name: "http match", origin: "http://192.168.2.1:9090", host: "192.168.2.1:9090", want: true},
		{name: "https match", origin: "https://router.example", host: "router.example", want: true},
		{name: "different host", origin: "http://192.168.2.2:9090", host: "192.168.2.1:9090", want: false},
		{name: "suffix confusion", origin: "http://evil.example/://192.168.2.1:9090", host: "192.168.2.1:9090", want: false},
		{name: "unsupported scheme", origin: "file://192.168.2.1:9090", host: "192.168.2.1:9090", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "http://"+test.host+"/api/screen", nil)
			request.Host = test.host
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if got := sameOrigin(request); got != test.want {
				t.Fatalf("sameOrigin() = %v, want %v", got, test.want)
			}
		})
	}
}
