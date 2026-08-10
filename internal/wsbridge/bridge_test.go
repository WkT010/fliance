package wsbridge

import (
	"math/big"
	"strings"
	"testing"

	"github.com/WkT010/nexa-exchange/internal/matching"
)

// TestFillIDDeterministic guards against the historical regression where the
// fill ID contained time.Now().UnixNano(), so the wallet-side processedFills
// dedupe never matched and replayed fills settled twice (loss of funds).
// The ID must be a pure function of the fill's immutable identity.
func TestFillIDDeterministic(t *testing.T) {
	price, _, _ := big.ParseFloat("50000.25", 10, 128, big.ToNearestEven)
	qty, _, _ := big.ParseFloat("0.123456789012345678", 10, 128, big.ToNearestEven)

	fill := &matching.FillNotification{
		TakerOrderID: "taker-1", MakerOrderID: "maker-1",
		Pair: "BTC/USDT", Price: price, Quantity: qty,
	}
	a := fillIDOf(fill)
	b := fillIDOf(fill)
	if a != b {
		t.Fatalf("fillID must be deterministic: %q vs %q", a, b)
	}
	want := "BTC/USDT:taker-1:maker-1:50000.250000000000000000:0.123456789012345678"
	if a != want {
		t.Fatalf("unexpected fillID format:\n got  %s\n want %s", a, want)
	}

	// A different fill (same pair/orders but different qty) must differ.
	qty2, _, _ := big.ParseFloat("0.123456789012345679", 10, 128, big.ToNearestEven)
	fill2 := &matching.FillNotification{
		TakerOrderID: "taker-1", MakerOrderID: "maker-1",
		Pair: "BTC/USDT", Price: price, Quantity: qty2,
	}
	if fillIDOf(fill2) == a {
		t.Fatal("distinct fills must have distinct IDs")
	}

	// Nil amounts must not panic and must stay deterministic.
	nilFill := &matching.FillNotification{TakerOrderID: "t", MakerOrderID: "m", Pair: "BTC/USDT"}
	if got := fillIDOf(nilFill); !strings.HasPrefix(got, "BTC/USDT:t:m:") {
		t.Fatalf("nil-amount fillID malformed: %q", got)
	}
	if fillIDOf(nilFill) != fillIDOf(nilFill) {
		t.Fatal("nil-amount fillID must be deterministic")
	}
}
