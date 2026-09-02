package movegen

import (
	"testing"

	"github.com/yourname/dyanis-chess-engine/internal/board"
)

func TestFoolsMateCheckmate(t *testing.T) {
	// Fool's Mate: 1. f3 e5 2. g4 Qh4# — fastest possible checkmate.
	const foolsMateFEN = "rnb1kbnr/pppp1ppp/8/4p3/6Pq/5P2/PPPPP2P/RNBQKBNR w KQkq - 1 3"
	b, err := board.FromFEN(foolsMateFEN)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}
	if got := GameStatus(b); got != Checkmate {
		t.Errorf("expected Checkmate, got %v", got)
	}
}

func TestStalemate(t *testing.T) {
	// Classic stalemate study: Black king on a8 has no legal move and
	// is not in check (White king b6, White queen c7 boxes it in).
	const stalemateFEN = "k7/2Q5/1K6/8/8/8/8/8 b - - 0 1"
	b, err := board.FromFEN(stalemateFEN)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}
	if got := GameStatus(b); got != Stalemate {
		t.Errorf("expected Stalemate, got %v", got)
	}
}

func TestOngoingAtStart(t *testing.T) {
	b := board.NewInitialBoard()
	if got := GameStatus(b); got != Ongoing {
		t.Errorf("expected Ongoing at the start position, got %v", got)
	}
}
