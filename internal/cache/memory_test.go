package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestMemoryRateLimitEnforced 验证超限后返回拒绝（此前恒返回 true 的缺陷）。
func TestMemoryRateLimitEnforced(t *testing.T) {
	c := NewMemoryCache(0)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		ok, err := c.RateLimit(ctx, "api:user1", 5, time.Minute)
		if err != nil {
			t.Fatalf("call %d unexpected error: %v", i, err)
		}
		if !ok {
			t.Fatalf("call %d should be allowed, got denied", i)
		}
	}

	for i := 0; i < 3; i++ {
		ok, err := c.RateLimit(ctx, "api:user1", 5, time.Minute)
		if err != nil {
			t.Fatalf("extra call %d unexpected error: %v", i, err)
		}
		if ok {
			t.Fatalf("extra call %d should be denied after exceeding limit", i)
		}
	}
}

// TestMemoryRateLimitPerKey 验证不同 key 的窗口相互独立。
func TestMemoryRateLimitPerKey(t *testing.T) {
	c := NewMemoryCache(0)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if ok, _ := c.RateLimit(ctx, "key-a", 3, time.Minute); !ok {
			t.Fatalf("key-a call %d should be allowed", i)
		}
	}
	if ok, _ := c.RateLimit(ctx, "key-a", 3, time.Minute); ok {
		t.Fatal("key-a should be denied after reaching limit")
	}
	if ok, _ := c.RateLimit(ctx, "key-b", 3, time.Minute); !ok {
		t.Fatal("key-b has its own window and should be allowed")
	}
}

// TestMemoryRateLimitWindowReset 验证窗口过期后计数重置、恢复放行。
func TestMemoryRateLimitWindowReset(t *testing.T) {
	c := NewMemoryCache(0)
	ctx := context.Background()
	window := 80 * time.Millisecond

	for i := 0; i < 2; i++ {
		if ok, _ := c.RateLimit(ctx, "k", 2, window); !ok {
			t.Fatalf("call %d should be allowed", i)
		}
	}
	if ok, _ := c.RateLimit(ctx, "k", 2, window); ok {
		t.Fatal("should be denied before window expires")
	}

	time.Sleep(window + 30*time.Millisecond)

	for i := 0; i < 2; i++ {
		if ok, _ := c.RateLimit(ctx, "k", 2, window); !ok {
			t.Fatalf("call %d should be allowed after window reset", i)
		}
	}
	if ok, _ := c.RateLimit(ctx, "k", 2, window); ok {
		t.Fatal("should be denied again after new window fills up")
	}
}

// TestMemoryRateLimitSlidingWindow 验证滑动窗口语义：
// 最早一批记录过期后即释放额度，而非整个窗口一起重置。
func TestMemoryRateLimitSlidingWindow(t *testing.T) {
	c := NewMemoryCache(0)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if ok, _ := c.RateLimit(ctx, "slide", 3, 200*time.Millisecond); !ok {
			t.Fatalf("call %d should be allowed", i)
		}
	}
	time.Sleep(120 * time.Millisecond)
	// 仍在第一批记录的窗口内，应被拒绝
	if ok, _ := c.RateLimit(ctx, "slide", 3, 200*time.Millisecond); ok {
		t.Fatal("should be denied while first batch is still inside window")
	}
	// 等第一批记录滑出窗口后应恢复放行
	time.Sleep(120 * time.Millisecond)
	if ok, _ := c.RateLimit(ctx, "slide", 3, 200*time.Millisecond); !ok {
		t.Fatal("should be allowed after earliest entries slid out of window")
	}
}

// TestMemoryRateLimitConcurrent 验证并发安全，且恰好放行 max 次。
func TestMemoryRateLimitConcurrent(t *testing.T) {
	c := NewMemoryCache(0)
	ctx := context.Background()

	const (
		goroutines = 200
		max        = 100
	)
	var wg sync.WaitGroup
	allowed := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := c.RateLimit(ctx, "burst", max, time.Minute)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			allowed <- ok
		}()
	}
	wg.Wait()
	close(allowed)

	count := 0
	for ok := range allowed {
		if ok {
			count++
		}
	}
	if count != max {
		t.Fatalf("expected exactly %d allowed under concurrency, got %d", max, count)
	}
}

// TestMemoryRateLimitEviction 验证 key 数量超过容量上限时不泄漏、功能正常。
func TestMemoryRateLimitEviction(t *testing.T) {
	c := NewMemoryCache(0)
	ctx := context.Background()

	for i := 0; i < rlMaxKeys+100; i++ {
		if ok, _ := c.RateLimit(ctx, fmt.Sprintf("k%d", i), 5, time.Minute); !ok {
			t.Fatalf("key %d should be allowed", i)
		}
	}

	c.rlMu.Lock()
	size := len(c.rlKeys)
	c.rlMu.Unlock()
	if size > rlMaxKeys {
		t.Fatalf("rlKeys size %d exceeds cap %d", size, rlMaxKeys)
	}

	// 淘汰后新 key 仍可正常限速
	if ok, _ := c.RateLimit(ctx, "fresh", 1, time.Minute); !ok {
		t.Fatal("fresh key should be allowed")
	}
	if ok, _ := c.RateLimit(ctx, "fresh", 1, time.Minute); ok {
		t.Fatal("fresh key should be denied on second call")
	}
}
