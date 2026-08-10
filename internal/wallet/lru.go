package wallet

import (
	"container/list"
	"time"
)

// fillEntry is one processed-fill record tracked by fillLRU.
type fillEntry struct {
	key        string
	accessedAt time.Time
}

// fillLRU is a bounded, timestamp-aware LRU set used to deduplicate fill
// settlements. It evicts the least-recently-accessed entry once the capacity
// is exceeded, and additionally expires entries older than ttl. Unlike the
// previous "randomly delete half the map" strategy, eviction is deterministic
// and never discards recently seen fills, so a replayed recent fill cannot be
// settled twice.
//
// fillLRU is NOT safe for concurrent use; the wallet Service guards it with
// its own mutex (the same locking pattern as before).
type fillLRU struct {
	items    map[string]*list.Element // key -> element in order list
	order    *list.List               // front = most recently accessed
	capacity int
	ttl      time.Duration
}

// newFillLRU creates a fill LRU with the given capacity and TTL. A ttl <= 0
// disables time-based expiry.
func newFillLRU(capacity int, ttl time.Duration) *fillLRU {
	if capacity <= 0 {
		capacity = defaultProcessedFillCapacity
	}
	return &fillLRU{
		items:    make(map[string]*list.Element, capacity),
		order:    list.New(),
		capacity: capacity,
		ttl:      ttl,
	}
}

// contains reports whether key is recorded. A hit refreshes the entry's
// access time and moves it to the front; an expired entry is removed and
// reported as absent.
func (l *fillLRU) contains(key string) bool {
	el, ok := l.items[key]
	if !ok {
		return false
	}
	e := el.Value.(*fillEntry)
	if l.isExpired(e) {
		l.removeElement(el)
		return false
	}
	e.accessedAt = time.Now()
	l.order.MoveToFront(el)
	return true
}

// add records key as processed, evicting entries if necessary.
func (l *fillLRU) add(key string) {
	if el, ok := l.items[key]; ok {
		e := el.Value.(*fillEntry)
		e.accessedAt = time.Now()
		l.order.MoveToFront(el)
		return
	}
	el := l.order.PushFront(&fillEntry{key: key, accessedAt: time.Now()})
	l.items[key] = el
	l.evict()
}

// len returns the number of recorded entries.
func (l *fillLRU) len() int { return l.order.Len() }

// setCapacity changes the capacity and evicts overflow entries.
func (l *fillLRU) setCapacity(n int) {
	if n <= 0 {
		n = defaultProcessedFillCapacity
	}
	l.capacity = n
	l.evict()
}

// setTTL changes the expiry window and evicts newly-expired entries.
func (l *fillLRU) setTTL(d time.Duration) {
	l.ttl = d
	l.evict()
}

func (l *fillLRU) isExpired(e *fillEntry) bool {
	return l.ttl > 0 && time.Since(e.accessedAt) > l.ttl
}

func (l *fillLRU) removeElement(el *list.Element) {
	l.order.Remove(el)
	delete(l.items, el.Value.(*fillEntry).key)
}

// evict drops the least-recently-accessed entries above capacity and any
// expired entries. Note: the list is ordered by access recency, so scanning
// from the back performs opportunistic TTL cleanup of the coldest entries.
func (l *fillLRU) evict() {
	for l.order.Len() > l.capacity {
		l.removeElement(l.order.Back())
	}
	for {
		back := l.order.Back()
		if back == nil {
			break
		}
		e := back.Value.(*fillEntry)
		if !l.isExpired(e) {
			break
		}
		l.removeElement(back)
	}
}
