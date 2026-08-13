package api

import (
	"fmt"
	"strconv"
)

// Numeric user IDs are produced by scrambling the users_uid_seq value through
// a balanced Feistel network followed by cycle-walking. The result is a
// bijection over each digit domain, so distinct sequence values always yield
// distinct UIDs while hiding issuance order (anti-enumeration).
//
// Domains (indices map into these, then shift into the digit range):
//
//	9 digits:  idx [0, 9e8)            -> uid = idx' + 1e8  (100000000..999999999)
//	10 digits: idx [9e8, 9.9e9)         -> uid = idx' + 1e9
//	11 digits: idx [9.9e9, 9.99e10)     -> uid = idx' + 1e10
//
// The sequence starts at 100000000; index = seq - 100000000.

const (
	uidSeqStart  int64 = 100000000
	uidDomain9   int64 = 900000000
	uidDomain10  int64 = 9000000000
	uidDomain11  int64 = 90000000000
	uidFeistelRounds   = 8 // >= 4 required; 8 rounds give strong diffusion
)

// uidFeistelKeys are the built-in per-round key constants. They are not
// secret (IDs only need to be non-enumerable, not confidential) but must stay
// stable: changing them would alter every future UID mapping.
var uidFeistelKeys = [uidFeistelRounds]uint64{
	0x9E3779B97F4A7C15, 0xC2B2AE3D27D4EB4F,
	0x165667B19E3779F9, 0x27D4EB2F165667C5,
	0xF7A35C81D836AC1F, 0x6C62272E07BB0142,
	0xB0757B27E8DFA5A2, 0x3FC7E91B52AC104D,
}

// feistelRound is the keyed round function: a cheap invertible-free mixer
// whose output bits feed the other half. Only avalanche quality matters —
// the network structure provides the bijectivity.
func feistelRound(x uint64, round int, halfBits uint) uint64 {
	h := x*uidFeistelKeys[round] + uidFeistelKeys[(round+3)%uidFeistelRounds]
	h ^= h >> 33
	h *= 0xFF51AFD7ED558CCD
	h ^= h >> 33
	return h & (1<<halfBits - 1)
}

// feistelPermute applies an R-round Feistel network over a 2*halfBits-bit
// block. This is a bijection over [0, 2^(2*halfBits)).
func feistelPermute(v uint64, halfBits uint) uint64 {
	mask := uint64(1)<<halfBits - 1
	l := (v >> halfBits) & mask
	r := v & mask
	for i := 0; i < uidFeistelRounds; i++ {
		l, r = r, (l^feistelRound(r, i, halfBits))&mask
	}
	// Undo the final swap so the mapping stays a clean permutation.
	return (r << halfBits) | l
}

// feistelCycleWalk permutes v within [0, domain) by repeatedly applying the
// block permutation until the value lands inside the domain. Cycle walking
// preserves bijectivity over the restricted domain.
func feistelCycleWalk(v uint64, domain uint64, halfBits uint) uint64 {
	for {
		v = feistelPermute(v, halfBits)
		if v < domain {
			return v
		}
	}
}

// uidDomainParams returns (domain, halfBits) for one digit width: the domain
// size and the Feistel half-width whose 2^(2*halfBits) block space covers it.
func uidDomainParams(digits int) (domain uint64, halfBits uint) {
	switch digits {
	case 9:
		return uint64(uidDomain9), 15 // 2^30 ≈ 1.07e9
	case 10:
		return uint64(uidDomain10), 17 // 2^34 ≈ 1.72e10
	default: // 11
		return uint64(uidDomain11), 19 // 2^38 ≈ 2.75e11
	}
}

// ScrambleUID maps a raw sequence value to a 9..11 digit numeric user ID
// string. It is a bijection within each digit domain, and domains are
// consumed in order (9 digits first, then 10, then 11), so outputs never
// collide across domains either.
func ScrambleUID(seq int64) (string, error) {
	idx := seq - uidSeqStart
	if idx < 0 {
		return "", fmt.Errorf("uid sequence %d below start %d", seq, uidSeqStart)
	}
	var digits int
	switch {
	case idx < uidDomain9:
		digits = 9
	case idx < uidDomain9+uidDomain10:
		digits = 10
		idx -= uidDomain9
	default:
		idx -= uidDomain9 + uidDomain10
		if idx >= uidDomain11 {
			return "", fmt.Errorf("uid sequence exhausted (index %d)", seq-uidSeqStart)
		}
		digits = 11
	}
	domain, halfBits := uidDomainParams(digits)
	scrambled := feistelCycleWalk(uint64(idx), domain, halfBits)
	shift := int64(100000000) // 9 digits
	if digits == 10 {
		shift = 1000000000
	} else if digits == 11 {
		shift = 10000000000
	}
	return strconv.FormatInt(int64(scrambled)+shift, 10), nil
}
