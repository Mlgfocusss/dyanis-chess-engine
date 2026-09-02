package perft

import (
	"testing"

	"github.com/yourname/dyanis-chess-engine/internal/board"
)

// Reference values are the standard, widely published perft numbers
// used to validate chess move generators. See the Chess Programming
// Wiki "Perft Results" page for the canonical list and more positions
// (e.g. "Position 3", "Position 4", "Position 5") worth adding here
// as the generator matures.

func TestPerftStartPosition(t *testing.T) {
	cases := []struct {
		depth int
		want  uint64
	}{
		{1, 20},
		{2, 400},
		{3, 8902},
		{4, 197281},
	}

	for _, c := range cases {
		b := board.NewInitialBoard()
		got := Perft(b, c.depth)
		if got != c.want {
			t.Errorf("perft(start, depth=%d) = %d, want %d", c.depth, got, c.want)
		}
	}
}

// TestPerftStartPositionDeep is separated out and skipped in -short
// mode because depth 5 is significantly slower with this straightforward,
// not-yet-optimized (copy-on-MakeMove) move generator.
func TestPerftStartPositionDeep(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow perft depth in -short mode")
	}
	b := board.NewInitialBoard()
	got := Perft(b, 5)
	want := uint64(4865609)
	if got != want {
		t.Errorf("perft(start, depth=5) = %d, want %d", got, want)
	}
}

// TestPerftKiwipete uses the famous "Kiwipete" position, specifically
// designed to exercise castling, en passant, and promotions together —
// the combination that most commonly exposes move generator bugs that
// the start position alone won't catch.
func TestPerftKiwipete(t *testing.T) {
	const kiwipeteFEN = "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1"

	cases := []struct {
		depth int
		want  uint64
	}{
		{1, 48},
		{2, 2039},
		{3, 97862},
	}

	for _, c := range cases {
		b, err := board.FromFEN(kiwipeteFEN)
		if err != nil {
			t.Fatalf("failed to parse Kiwipete FEN: %v", err)
		}
		got := Perft(b, c.depth)
		if got != c.want {
			t.Errorf("perft(kiwipete, depth=%d) = %d, want %d", c.depth, got, c.want)
		}
	}
}
