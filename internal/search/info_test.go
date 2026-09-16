package search

import (
	"testing"
	"time"

	"github.com/yourname/dyanis-chess-engine/internal/board"
	"github.com/yourname/dyanis-chess-engine/internal/book"
)

func TestScoreToUCIFormatsOrdinaryScoreAsCentipawns(t *testing.T) {
	if got, want := ScoreToUCI(37), "cp 37"; got != want {
		t.Errorf("ScoreToUCI(37) = %q, want %q", got, want)
	}
	if got, want := ScoreToUCI(-120), "cp -120"; got != want {
		t.Errorf("ScoreToUCI(-120) = %q, want %q", got, want)
	}
}

func TestScoreToUCIFormatsMateInOne(t *testing.T) {
	// A mate delivered by White's very next move is detected at ply 1
	// (see negamax's checkmate leaf) and propagates up as
	// MateScore-1 — exactly what BestMove/BestMoveInfo return for the
	// a1a8 back-rank mate elsewhere in this package's tests.
	score := MateScore - 1
	if got, want := ScoreToUCI(score), "mate 1"; got != want {
		t.Errorf("ScoreToUCI(%d) = %q, want %q", score, got, want)
	}
}

func TestScoreToUCIFormatsBeingMatedAsNegative(t *testing.T) {
	score := -(MateScore - 1)
	if got, want := ScoreToUCI(score), "mate -1"; got != want {
		t.Errorf("ScoreToUCI(%d) = %q, want %q", score, got, want)
	}
}

func TestBestMoveInfoReportsMateInOne(t *testing.T) {
	const fen = "6k1/5ppp/8/8/8/8/8/R5K1 w - - 0 1"
	b, err := board.FromFEN(fen)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}

	var got SearchInfo
	m, err := BestMoveInfo(b, 1, func(info SearchInfo) { got = info })
	if err != nil {
		t.Fatalf("BestMoveInfo: %v", err)
	}
	if want := "a1a8"; m.String() != want {
		t.Errorf("move = %s, want %s", m, want)
	}
	if got.Depth != 1 {
		t.Errorf("info.Depth = %d, want 1", got.Depth)
	}
	if ScoreToUCI(got.Score) != "mate 1" {
		t.Errorf("info.Score = %d (%s), want a mate-in-1 score", got.Score, ScoreToUCI(got.Score))
	}
	if len(got.PV) == 0 || got.PV[0].String() != "a1a8" {
		t.Errorf("info.PV = %v, want it to start with a1a8", got.PV)
	}
	if got.Nodes <= 0 {
		t.Errorf("info.Nodes = %d, want a positive node count", got.Nodes)
	}
}

func TestBestMoveTimedInfoReportsOneCallbackPerCompletedDepth(t *testing.T) {
	b := board.NewInitialBoard()

	var depths []int
	_, err := BestMoveTimedInfo(b, 3, time.Second, func(info SearchInfo) {
		depths = append(depths, info.Depth)
	})
	if err != nil {
		t.Fatalf("BestMoveTimedInfo: %v", err)
	}
	if len(depths) != 3 {
		t.Fatalf("got %d info callbacks (%v), want exactly 3 (depths 1..3)", len(depths), depths)
	}
	for i, d := range depths {
		if d != i+1 {
			t.Errorf("depths[%d] = %d, want %d", i, d, i+1)
		}
	}
}

func TestBestMoveTimedInfoNodesNeverDecreases(t *testing.T) {
	// tt.nodes is a running total shared across every depth in one
	// BestMoveTimedInfo call (see TranspositionTable.nodes's comment)
	// — each successive callback should report at least as many nodes
	// as the last, never fewer.
	b := board.NewInitialBoard()

	prev := int64(-1)
	_, err := BestMoveTimedInfo(b, 3, time.Second, func(info SearchInfo) {
		if info.Nodes < prev {
			t.Errorf("nodes decreased at depth %d: %d < previous %d", info.Depth, info.Nodes, prev)
		}
		prev = info.Nodes
	})
	if err != nil {
		t.Fatalf("BestMoveTimedInfo: %v", err)
	}
}

func TestBestMoveWithBookInfoSkipsCallbackOnBookHit(t *testing.T) {
	start := board.NewInitialBoard()
	bk := book.New([]book.Entry{
		{Key: start.Hash(), Move: encode(4, 1, 4, 3, 0), Weight: 1}, // e2e4
	})

	called := false
	m, fromBook, err := BestMoveWithBookInfo(start, 3, bk, func(SearchInfo) { called = true })
	if err != nil {
		t.Fatalf("BestMoveWithBookInfo: %v", err)
	}
	if !fromBook {
		t.Fatal("expected the book move to be played")
	}
	if m.String() != "e2e4" {
		t.Errorf("move = %s, want e2e4", m)
	}
	if called {
		t.Error("onInfo should not be called when the book supplies the move (no search ran)")
	}
}
