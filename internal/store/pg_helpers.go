package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// nowText returns the current unix nano as a decimal string.
func nowText() string { return fmt.Sprintf("%d", time.Now().UnixNano()) }

// nowNano returns the current unix nano timestamp.
func nowNano() int64 { return time.Now().UnixNano() }

// randSuffix returns 6 hex chars of randomness for ID uniqueness.
func randSuffix() string {
	b := make([]byte, 3)
	rand.Read(b)
	return hex.EncodeToString(b)
}
