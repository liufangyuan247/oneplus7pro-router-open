package main

import (
	"math"
	"testing"
	"time"
)

func TestNetworkSamplerUsesFiveSecondWindow(t *testing.T) {
	server := &Server{
		netStats: make(map[string]netCounter),
		netRates: make(map[string]netRate),
	}
	start := time.Unix(1_700_000_000, 0)
	server.sampleNetwork(start, map[string]netCounter{"ppp0": {}})

	// One one-second burst followed by four quiet seconds should be presented
	// as its honest five-second average, not as a client-dependent spike.
	server.sampleNetwork(start.Add(time.Second), map[string]netCounter{"ppp0": {rx: 50_000_000, tx: 5_000_000}})
	for second := 2; second <= 5; second++ {
		server.sampleNetwork(start.Add(time.Duration(second)*time.Second), map[string]netCounter{"ppp0": {rx: 50_000_000, tx: 5_000_000}})
	}

	stats, rates := server.networkSnapshot()
	if stats["ppp0"].rx != 50_000_000 {
		t.Fatalf("rx total = %d, want 50000000", stats["ppp0"].rx)
	}
	if diff := math.Abs(rates["ppp0"].rx - 10_000_000); diff > 0.01 {
		t.Fatalf("rx rate = %f, want 10000000", rates["ppp0"].rx)
	}
	if diff := math.Abs(rates["ppp0"].tx - 1_000_000); diff > 0.01 {
		t.Fatalf("tx rate = %f, want 1000000", rates["ppp0"].tx)
	}

	server.sampleNetwork(start.Add(6*time.Second), map[string]netCounter{"ppp0": {rx: 50_000_000, tx: 5_000_000}})
	_, rates = server.networkSnapshot()
	if rates["ppp0"] != (netRate{}) {
		t.Fatalf("rate after burst leaves window = %#v, want zero", rates["ppp0"])
	}
}

func TestNetworkSamplerDropsCounterReset(t *testing.T) {
	server := &Server{
		netStats: make(map[string]netCounter),
		netRates: make(map[string]netRate),
	}
	start := time.Unix(1_700_000_000, 0)
	server.sampleNetwork(start, map[string]netCounter{"ppp0": {rx: 100, tx: 100}})
	server.sampleNetwork(start.Add(time.Second), map[string]netCounter{"ppp0": {rx: 10, tx: 10}})

	_, rates := server.networkSnapshot()
	if rates["ppp0"] != (netRate{}) {
		t.Fatalf("rate after counter reset = %#v, want zero", rates["ppp0"])
	}
}
