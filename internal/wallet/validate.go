package wallet

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/sha3"
)

// This file implements strict, chain-specific address format validation. It is
// independent of any BlockchainClient implementation so that withdrawals are
// validated even when a development MockBlockchainClient is registered.

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

// ValidateWithdrawalAddress performs strict format validation for a withdrawal
// destination address on a known chain. It returns an error wrapping
// ErrInvalidAddress when the address is malformed. For assets without a known
// strict format it returns nil; callers must additionally consult the
// blockchain client's own validation for such assets.
func ValidateWithdrawalAddress(asset, address string) error {
	switch strings.ToUpper(strings.TrimSpace(asset)) {
	case "BTC":
		if !ValidateBTCAddress(address) {
			return fmt.Errorf("%w: malformed BTC address", ErrInvalidAddress)
		}
	case "ETH", "POLYGON", "MATIC":
		if !ValidateETHAddress(address) {
			return fmt.Errorf("%w: malformed %s address", ErrInvalidAddress, strings.ToUpper(strings.TrimSpace(asset)))
		}
	default:
		// No strict format registered for this asset: fall back to the
		// client-level check performed by the caller.
		return nil
	}
	return nil
}

// ---------------------------------------------------------------------------
// Bitcoin: base58check (P2PKH / P2SH) and bech32 (segwit)
// ---------------------------------------------------------------------------

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58Decode decodes a base58 string, preserving leading zero bytes
// (represented by '1' characters).
func base58Decode(s string) ([]byte, bool) {
	x := new(big.Int)
	base := big.NewInt(58)
	for _, c := range s {
		idx := strings.IndexRune(base58Alphabet, c)
		if idx < 0 {
			return nil, false
		}
		x.Mul(x, base)
		x.Add(x, big.NewInt(int64(idx)))
	}
	body := x.Bytes()
	leadingZeros := 0
	for _, c := range s {
		if c != '1' {
			break
		}
		leadingZeros++
	}
	out := make([]byte, leadingZeros+len(body))
	copy(out[leadingZeros:], body)
	return out, true
}

// base58CheckDecode decodes and verifies the 4-byte double-SHA256 checksum.
func base58CheckDecode(s string) (version byte, payload []byte, ok bool) {
	data, ok := base58Decode(s)
	if !ok || len(data) < 5 {
		return 0, nil, false
	}
	payload, checksum := data[:len(data)-4], data[len(data)-4:]
	h1 := sha256.Sum256(payload)
	h2 := sha256.Sum256(h1[:])
	if !bytes.Equal(h2[:4], checksum) {
		return 0, nil, false
	}
	return payload[0], payload[1:], true
}

// ValidateBTCAddress validates a Bitcoin address (mainnet or testnet):
//   - base58check P2PKH: mainnet '1' (version 0x00), testnet 'm'/'n' (0x6F)
//   - base58check P2SH:  mainnet '3' (version 0x05), testnet '2' (0xC4)
//   - bech32 segwit:     mainnet 'bc1', testnet 'tb1'
func ValidateBTCAddress(addr string) bool {
	if addr == "" {
		return false
	}
	if strings.HasPrefix(addr, "bc1") || strings.HasPrefix(addr, "tb1") {
		hrp := "bc"
		if strings.HasPrefix(addr, "tb1") {
			hrp = "tb"
		}
		return validateBech32Address(addr, hrp)
	}
	// base58check legacy addresses are 25-34 characters.
	if len(addr) < 25 || len(addr) > 34 {
		return false
	}
	version, payload, ok := base58CheckDecode(addr)
	if !ok || len(payload) != 20 {
		return false
	}
	switch version {
	case 0x00: // mainnet P2PKH -> '1'
		return strings.HasPrefix(addr, "1")
	case 0x05: // mainnet P2SH -> '3'
		return strings.HasPrefix(addr, "3")
	case 0x6F: // testnet P2PKH -> 'm' or 'n'
		return strings.HasPrefix(addr, "m") || strings.HasPrefix(addr, "n")
	case 0xC4: // testnet P2SH -> '2'
		return strings.HasPrefix(addr, "2")
	default:
		return false
	}
}

const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

func bech32Polymod(values []byte) uint32 {
	gen := []uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := uint32(1)
	for _, v := range values {
		b := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i := 0; i < 5; i++ {
			if (b>>uint(i))&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

// validateBech32Address verifies a segwit bech32 address: case consistency,
// HRP, polymod checksum and witness program length (BIP-173 / BIP-141).
func validateBech32Address(addr, expHRP string) bool {
	if len(addr) < 8 || len(addr) > 90 {
		return false
	}
	if addr != strings.ToLower(addr) && addr != strings.ToUpper(addr) {
		return false // mixed case is invalid for bech32
	}
	addr = strings.ToLower(addr)
	pos := strings.LastIndex(addr, "1")
	if pos < 1 || pos+7 > len(addr) {
		return false
	}
	hrp, dataPart := addr[:pos], addr[pos+1:]
	if hrp != expHRP {
		return false
	}
	data := make([]byte, 0, len(dataPart))
	for _, c := range dataPart {
		idx := strings.IndexRune(bech32Charset, c)
		if idx < 0 {
			return false
		}
		data = append(data, byte(idx))
	}
	// Checksum verification.
	expanded := make([]byte, 0, 2*len(hrp)+1+len(data))
	for _, c := range hrp {
		expanded = append(expanded, byte(c)>>5)
	}
	expanded = append(expanded, 0)
	for _, c := range hrp {
		expanded = append(expanded, byte(c)&31)
	}
	expanded = append(expanded, data...)
	if bech32Polymod(expanded) != 1 {
		return false
	}
	// Witness program: version (1 symbol) + program, excluding 6-symbol checksum.
	witnessVersion := data[0]
	program := data[1 : len(data)-6]
	conv, ok := convertBits(program, 5, 8, false)
	if !ok {
		return false
	}
	if witnessVersion == 0 {
		return len(conv) == 20 || len(conv) == 32
	}
	return witnessVersion >= 1 && witnessVersion <= 16 && len(conv) >= 2 && len(conv) <= 40
}

// convertBits regroups a byte slice from `from`-bit groups into `to`-bit groups.
func convertBits(data []byte, from, to uint, pad bool) ([]byte, bool) {
	acc := uint32(0)
	bits := uint(0)
	out := make([]byte, 0, len(data)*int(from)/int(to)+1)
	maxV := uint32(1<<to) - 1
	for _, b := range data {
		if uint32(b)>>from != 0 {
			return nil, false
		}
		acc = acc<<from | uint32(b)
		bits += from
		for bits >= to {
			bits -= to
			out = append(out, byte(acc>>bits&maxV))
		}
	}
	if pad {
		if bits > 0 {
			out = append(out, byte(acc<<(to-bits)&maxV))
		}
	} else if bits >= from || acc<<(to-bits)&maxV != 0 {
		return nil, false
	}
	return out, true
}

// ---------------------------------------------------------------------------
// Ethereum / Polygon (EVM): 0x + 40 hex digits, EIP-55 checksum enforcement
// ---------------------------------------------------------------------------

// ValidateETHAddress validates an Ethereum / Polygon (EVM) address: it must be
// "0x" followed by 40 hexadecimal digits. All-lowercase and all-uppercase
// forms are accepted; mixed-case addresses MUST pass the EIP-55 checksum.
func ValidateETHAddress(addr string) bool {
	if len(addr) != 42 || !(strings.HasPrefix(addr, "0x") || strings.HasPrefix(addr, "0X")) {
		return false
	}
	body := addr[2:]
	for _, c := range body {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	if body == strings.ToLower(body) || body == strings.ToUpper(body) {
		return true // no checksum information to verify
	}
	return eip55Checksum(body) == body
}

// eip55Checksum returns the EIP-55 canonical mixed-case form of a 40-char hex
// address body (without the 0x prefix).
func eip55Checksum(body string) string {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(strings.ToLower(body)))
	hash := h.Sum(nil)
	out := []byte(strings.ToLower(body))
	for i := 0; i < len(out); i++ {
		c := out[i]
		if c < 'a' || c > 'f' {
			continue
		}
		var nibble byte
		if i%2 == 0 {
			nibble = hash[i/2] >> 4
		} else {
			nibble = hash[i/2] & 0x0f
		}
		if nibble >= 8 {
			out[i] = c - 32 // uppercase
		}
	}
	return string(out)
}
