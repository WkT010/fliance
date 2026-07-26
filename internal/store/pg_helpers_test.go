package store

import (
	"strconv"
	"testing"
	"time"
)

func TestNowText(t *testing.T) {
	before := time.Now().UnixNano()
	s := nowText()
	after := time.Now().UnixNano()
	if s == "" {
		t.Fatal("nowText returned empty string")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("nowText returned %q which is not a valid int64: %v", s, err)
	}
	if n < before || n > after {
		t.Fatalf("nowText value %d not in expected range [%d, %d]", n, before, after)
	}
}

func TestRandSuffix(t *testing.T) {
	s := randSuffix()
	if len(s) != 6 {
		t.Fatalf("expected 6-char hex string, got %q (len=%d)", s, len(s))
	}
	if _, err := strconv.ParseInt(s, 16, 64); err != nil {
		t.Fatalf("randSuffix returned %q which is not valid hex: %v", s, err)
	}
	// Successive calls should produce different values (collision probability
	// is 1/16^6 ≈ 6e-8).
	a := randSuffix()
	b := randSuffix()
	if a == b {
		t.Fatalf("expected different suffixes on successive calls, got %q twice", a)
	}
}
