package search

import (
	"testing"
	"time"

	"github.com/yourname/dyanis-chess-engine/internal/board"
)

// Re-verify the two existing fixed-depth behaviors still hold now
// that BestMove is implemented via the unified negamax + TT, not its
// own separate root loop — this is the "did the refactor change
// anything observable" check.

func TestBestMoveStillFindsMateInOneWithTT(t *testing.T) {
	const fen = "6k1/5ppp/8/8/8/8/8/R5K1 w - - 0 1"
	b, err := board.FromFEN(fen)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}
	m, err := BestMove(b, 1)
	if err != nil {
		t.Fatalf("BestMove: %v", err)
	}
	if want := "a1a8"; m.String() != want {
		t.Errorf("expected mate-in-1 move %s, got %s", want, m)
	}
}

func TestBestMoveTimedFindsMateInOne(t *testing.T) {
	const fen = "6k1/5ppp/8/8/8/8/8/R5K1 w - - 0 1"
	b, err := board.FromFEN(fen)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}
	m, err := BestMoveTimed(b, 5, time.Second)
	if err != nil {
		t.Fatalf("BestMoveTimed: %v", err)
	}
	if want := "a1a8"; m.String() != want {
		t.Errorf("expected mate-in-1 move %s, got %s", want, m)
	}
}

func TestBestMoveTimedAlwaysCompletesDepthOne(t *testing.T) {
	// Even a zero (already-expired) budget must still return depth 1's
	// move rather than a zero-value Move — depth 1 always runs.
	b := board.NewInitialBoard()
	m, err := BestMoveTimed(b, 5, 0)
	if err != nil {
		t.Fatalf("BestMoveTimed: %v", err)
	}
	if m.String() == "" {
		t.Error("expected a real move even with a zero time budget")
	}
}

func TestBestMoveTimedRespectsMaxDepth(t *testing.T) {
	// A generous budget but maxDepth=1 should behave like BestMove(b,1):
	// still finds a mate in 1 (found at depth 1), not search deeper.
	const fen = "6k1/5ppp/8/8/8/8/8/R5K1 w - - 0 1"
	b, err := board.FromFEN(fen)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}
	m, err := BestMoveTimed(b, 1, 10*time.Second)
	if err != nil {
		t.Fatalf("BestMoveTimed: %v", err)
	}
	if want := "a1a8"; m.String() != want {
		t.Errorf("expected mate-in-1 move %s, got %s", want, m)
	}
}

func TestTranspositionTableDoesNotChangeTheAnswer(t *testing.T) {
	// Same "capture the hanging rook" position search_test.go already
	// covers at depth 2 for BestMove — re-run it at a deeper depth
	// where transpositions actually start to occur, to make sure the
	// TT is only an optimization and never changes the result.
	const fen = "3r2k1/8/8/8/8/8/8/3Q2K1 w - - 0 1"
	b, err := board.FromFEN(fen)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}
	m, err := BestMove(b, 4)
	if err != nil {
		t.Fatalf("BestMove: %v", err)
	}
	if want := "d1d8"; m.String() != want {
		t.Errorf("expected the engine to capture the hanging rook with %s, got %s", want, m)
	}
}

func TestNegamaxNilTableStillWorks(t *testing.T) {
	// negamax must not panic or misbehave with tt == nil — it's a
	// documented supported mode, not just an internal implementation
	// detail of BestMove/BestMoveTimed. isRoot=true here since this is
	// a standalone top-level call, not one made from within another
	// negamax's move loop.
	b := board.NewInitialBoard()
	score, m := negamax(b, 2, -Infinity, Infinity, true, nil)
	if m.String() == "" {
		t.Error("expected a real move from negamax with tt == nil")
	}
	if score <= -Infinity || score >= Infinity {
		t.Errorf("score %d out of a sane range", score)
	}
}
