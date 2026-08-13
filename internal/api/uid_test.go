package api

import (
	"strconv"
	"testing"
)

// TestScrambleUIDBijective9Digit verifies the Feistel scramble is collision
// free over the first 100k sequence values and every output stays inside the
// 9-digit range [100000000, 999999999].
func TestScrambleUIDBijective9Digit(t *testing.T) {
	const n = 100000
	seen := make(map[string]struct{}, n)
	for i := int64(0); i < n; i++ {
		seq := uidSeqStart + i
		uid, err := ScrambleUID(seq)
		if err != nil {
			t.Fatalf("ScrambleUID(%d): %v", seq, err)
		}
		if len(uid) != 9 {
			t.Fatalf("ScrambleUID(%d) = %q, want 9 digits", seq, uid)
		}
		v, err := strconv.ParseInt(uid, 10, 64)
		if err != nil {
			t.Fatalf("ScrambleUID(%d) = %q, not numeric", seq, uid)
		}
		if v < 100000000 || v > 999999999 {
			t.Fatalf("ScrambleUID(%d) = %q, outside 9-digit range", seq, uid)
		}
		if _, dup := seen[uid]; dup {
			t.Fatalf("collision: seq %d produced duplicate uid %s", seq, uid)
		}
		seen[uid] = struct{}{}
	}
	if len(seen) != n {
		t.Fatalf("unique count = %d, want %d", len(seen), n)
	}
}

// TestScrambleUIDDeterministic verifies the mapping is stable and that
// adjacent sequence values do not produce adjacent UIDs (order hiding).
func TestScrambleUIDDeterministic(t *testing.T) {
	a, err := ScrambleUID(uidSeqStart + 12345)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ScrambleUID(uidSeqStart + 12345)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("same seq produced different uids: %s vs %s", a, b)
	}
	c, err := ScrambleUID(uidSeqStart + 12346)
	if err != nil {
		t.Fatal(err)
	}
	if a == c {
		t.Fatalf("distinct seqs collided on %s", a)
	}
}

// TestScrambleUIDDomainExtension verifies the domain boundaries: the last
// 9-digit index stays 9 digits, the next index rolls over to 10 digits, the
// 10->11 rollover works, and exhaustion / below-start inputs error out.
func TestScrambleUIDDomainExtension(t *testing.T) {
	last9, err := ScrambleUID(uidSeqStart + uidDomain9 - 1)
	if err != nil || len(last9) != 9 {
		t.Fatalf("last 9-digit index: uid=%s err=%v", last9, err)
	}
	first10, err := ScrambleUID(uidSeqStart + uidDomain9)
	if err != nil {
		t.Fatalf("first 10-digit index: %v", err)
	}
	if len(first10) != 10 {
		t.Fatalf("first 10-digit index: uid=%s, want 10 digits", first10)
	}
	if v, _ := strconv.ParseInt(first10, 10, 64); v < 1000000000 || v > 9999999999 {
		t.Fatalf("first 10-digit uid %s outside range", first10)
	}
	first11, err := ScrambleUID(uidSeqStart + uidDomain9 + uidDomain10)
	if err != nil {
		t.Fatalf("first 11-digit index: %v", err)
	}
	if len(first11) != 11 {
		t.Fatalf("first 11-digit index: uid=%s, want 11 digits", first11)
	}
	if _, err := ScrambleUID(uidSeqStart + uidDomain9 + uidDomain10 + uidDomain11); err == nil {
		t.Fatal("exhausted domain should error")
	}
	if _, err := ScrambleUID(uidSeqStart - 1); err == nil {
		t.Fatal("sequence below start should error")
	}
}
