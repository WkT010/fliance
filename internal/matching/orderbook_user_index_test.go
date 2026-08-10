package matching

import (
	"testing"
)

// TestGetOrdersByUserIndex verifies the per-user index returns exactly the
// resting orders of a user and stays in sync across add/remove/pop paths.
func TestGetOrdersByUserIndex(t *testing.T) {
	ob := NewOrderBook("BTC/USDT")
	a1 := createLimitOrderUser("alice", Buy, "50000", "1.0")
	a2 := createLimitOrderUser("alice", Sell, "51000", "1.0")
	b1 := createLimitOrderUser("bob", Buy, "49000", "1.0")
	ob.Add(a1)
	ob.Add(a2)
	ob.Add(b1)

	alice := ob.GetOrdersByUser("alice")
	if len(alice) != 2 {
		t.Fatalf("expected 2 orders for alice, got %d", len(alice))
	}
	bob := ob.GetOrdersByUser("bob")
	if len(bob) != 1 || bob[0].ID != b1.ID {
		t.Fatalf("expected exactly bob's order, got %d", len(bob))
	}
	if got := ob.GetOrdersByUser("nobody"); len(got) != 0 {
		t.Errorf("expected empty result for unknown user, got %d", len(got))
	}

	// Cancel path (removeLocked) must update the index.
	ob.Remove(a1.ID)
	alice = ob.GetOrdersByUser("alice")
	if len(alice) != 1 || alice[0].ID != a2.ID {
		t.Fatalf("expected only a2 after cancel, got %d orders", len(alice))
	}

	// PopBestAsk removes a2 (lowest ask) and must update the index too.
	popped := ob.PopBestAsk()
	if popped == nil || popped.ID != a2.ID {
		t.Fatalf("expected PopBestAsk to return a2, got %v", popped)
	}
	if got := ob.GetOrdersByUser("alice"); len(got) != 0 {
		t.Errorf("expected alice's index empty after pop, got %d", len(got))
	}
	if _, ok := ob.userOrders["alice"]; ok {
		t.Error("expected empty user entry to be pruned from the index")
	}
}

// TestGetOrdersByUserIndexAfterRestore ensures snapshot restore rebuilds the
// per-user index from scratch.
func TestGetOrdersByUserIndexAfterRestore(t *testing.T) {
	src := NewOrderBook("BTC/USDT")
	src.Add(createLimitOrderUser("alice", Buy, "50000", "1.0"))
	src.Add(createLimitOrderUser("alice", Sell, "51000", "1.0"))
	src.Add(createLimitOrderUser("bob", Buy, "49000", "1.0"))
	snap := src.Snapshot()

	dst := NewOrderBook("BTC/USDT")
	if err := dst.Restore(snap); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := dst.GetOrdersByUser("alice"); len(got) != 2 {
		t.Errorf("expected 2 restored orders for alice, got %d", len(got))
	}
	if got := dst.GetOrdersByUser("bob"); len(got) != 1 {
		t.Errorf("expected 1 restored order for bob, got %d", len(got))
	}
	// Index must keep working after post-restore mutations.
	dst.Remove(dst.GetOrdersByUser("bob")[0].ID)
	if got := dst.GetOrdersByUser("bob"); len(got) != 0 {
		t.Errorf("expected bob's index empty after post-restore cancel, got %d", len(got))
	}
}

// TestEngineUserIndexLifecycle exercises the full engine paths: resting add,
// cancel via channel, and maker removal on full fill all update the index.
func TestEngineUserIndexLifecycle(t *testing.T) {
	e := setupEngine()
	defer e.Stop()

	// Resting orders for two users.
	submitSync(e, createLimitOrderUser("alice", Buy, "49000", "1.0"))
	submitSync(e, createLimitOrderUser("bob", Buy, "48000", "1.0"))
	if got := e.OrderBook.GetOrdersByUser("alice"); len(got) != 1 {
		t.Fatalf("expected 1 resting order for alice, got %d", len(got))
	}

	// Cancel via the engine's cancel channel.
	aliceOrder := e.OrderBook.GetOrdersByUser("alice")[0]
	if _, err := e.Cancel(aliceOrder.ID, "alice"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got := e.OrderBook.GetOrdersByUser("alice"); len(got) != 0 {
		t.Errorf("expected alice's index empty after cancel, got %d", len(got))
	}

	// Full fill removes the maker from the index.
	maker := submitSync(e, createLimitOrderUser("alice", Sell, "50000", "1.0"))
	if maker.Status != New {
		t.Fatalf("expected maker to rest, got %s", maker.Status)
	}
	submitSync(e, createLimitOrderUser("bob", Buy, "50000", "1.0"))
	waitForTrades(e.Trades, 1)
	if got := e.OrderBook.GetOrdersByUser("alice"); len(got) != 0 {
		t.Errorf("expected alice's index empty after full fill, got %d", len(got))
	}
	// bob's buy fully filled too, so his resting bid from earlier plus the
	// taker should leave only the 48000 bid.
	bobOrders := e.OrderBook.GetOrdersByUser("bob")
	if len(bobOrders) != 1 || bobOrders[0].UserID != "bob" {
		t.Errorf("expected only bob's resting 48000 bid, got %d orders", len(bobOrders))
	}
}
