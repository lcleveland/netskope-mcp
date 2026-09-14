package main

import "testing"

// The warning this drives is the only thing standing between a typo in
// listenAddress and an unauthenticated tenant-administration endpoint.
func TestLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8231": true,
		"127.0.1.1:8231": true,
		"[::1]:8231":     true,
		"localhost:8231": true,
		"localhost":      true,
		"0.0.0.0:8231":   false,
		"[::]:8231":      false,
		":8231":          false,
		"10.0.0.5:8231":  false,
		"":               false,
	} {
		if got := loopback(addr); got != want {
			t.Errorf("loopback(%q) = %v, want %v", addr, got, want)
		}
	}
}
