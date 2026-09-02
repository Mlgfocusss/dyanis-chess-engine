package search

import (
	"testing"
	"time"

	"github.com/yourname/dyanis-chess-engine/internal/board"
	"github.com/yourname/dyanis-chess-engine/internal/book"
)

// encode mirrors Polyglot's 16-bit move packing (same scheme as
// book_test.go), for building synthetic book entries without hand
// computing bit patterns.
func encode(fromFile, fromRank, toFile, toRank int, promo uint16) uint16 {
	return uint16(toFile) | uint16(toRank)<<3 | uint16(fromFile)<<6 | uint16(fromRank)<<9 | promo<<12
}

func TestBestMoveWithBookPlaysTheOnlyBookMove(t *testing.T) {
	start := board.NewInitialBoard()
	bk := book.New([]book.Entry{
		{Key: start.Hash(), Move: encode(4, 1, 4, 3, 0), Weight: 1}, // e2e4
	})

	m, fromBook, err := BestMoveWithBook(start, 3, bk)
	if err != nil {
		t.Fatalf("BestMoveWithBook: %v", err)
	}
	if !fromBook {
		t.Error("fromBook = false, want true (position is in the book)")
	}
	if m.String() != "e2e4" {
		t.Errorf("move = %q, want %q", m.String(), "e2e4")
	}
}

func TestBestMoveWithBookFallsThroughWhenPositionNotInBook(t *testing.T) {
	start := board.NewInitialBoard()
	// A book with an entry for a *different* position (some arbitrary
	// key), so LookupBoard on the starting position finds nothing.
	bk := book.New([]book.Entry{
		{Key: 0xdeadbeef, Move: encode(4, 1, 4, 3, 0), Weight: 1},
	})

	m, fromBook, err := BestMoveWithBook(start, 1, bk)
	if err != nil {
		t.Fatalf("BestMoveWithBook: %v", err)
	}
	if fromBook {
		t.Error("fromBook = true, want false (position isn't in the book)")
	}
	if m.String() == "" {
		t.Error("expected a real move from the fallback search")
	}
}

func TestBestMoveWithBookNilBookFallsThroughToSearch(t *testing.T) {
	start := board.NewInitialBoard()

	m, fromBook, err := BestMoveWithBook(start, 1, nil)
	if err != nil {
		t.Fatalf("BestMoveWithBook: %v", err)
	}
	if fromBook {
		t.Error("fromBook = true, want false (no book was given)")
	}
	if m.String() == "" {
		t.Error("expected a real move from search")
	}
}

func TestPickBookMoveSkipsZeroWeightEntries(t *testing.T) {
	start := board.NewInitialBoard()
	bk := book.New([]book.Entry{
		{Key: start.Hash(), Move: encode(4, 1, 4, 3, 0), Weight: 0}, // e2e4, deleted
	})

	if _, ok := pickBookMove(start, bk); ok {
		t.Error("pickBookMove: expected no move (only entry has weight 0)")
	}
}

func TestPickBookMoveIsDeterministicWhenOnlyOneEntryHasWeight(t *testing.T) {
	start := board.NewInitialBoard()
	bk := book.New([]book.Entry{
		{Key: start.Hash(), Move: encode(6, 0, 5, 2, 0), Weight: 0},  // Ng1-f3, deleted
		{Key: start.Hash(), Move: encode(4, 1, 4, 3, 0), Weight: 50}, // e2e4, the only live entry
		{Key: start.Hash(), Move: encode(3, 1, 3, 3, 0), Weight: 0},  // d2d4, deleted
	})

	// Weighted random selection still collapses to a single possible
	// outcome when only one entry has nonzero weight; run it several
	// times to make sure that's actually true and not a fluke.
	for i := 0; i < 20; i++ {
		m, ok := pickBookMove(start, bk)
		if !ok {
			t.Fatal("pickBookMove: expected a move, got none")
		}
		if m.String() != "e2e4" {
			t.Errorf("move = %q, want %q (the only nonzero-weight entry)", m.String(), "e2e4")
		}
	}
}

func TestBestMoveTimedWithBookPlaysTheOnlyBookMove(t *testing.T) {
	start := board.NewInitialBoard()
	bk := book.New([]book.Entry{
		{Key: start.Hash(), Move: encode(4, 1, 4, 3, 0), Weight: 1}, // e2e4
	})

	m, fromBook, err := BestMoveTimedWithBook(start, 5, time.Second, bk)
	if err != nil {
		t.Fatalf("BestMoveTimedWithBook: %v", err)
	}
	if !fromBook {
		t.Error("fromBook = false, want true (position is in the book)")
	}
	if m.String() != "e2e4" {
		t.Errorf("move = %q, want %q", m.String(), "e2e4")
	}
}

func TestBestMoveTimedWithBookFallsThroughToTimedSearch(t *testing.T) {
	start := board.NewInitialBoard()
	bk := book.New([]book.Entry{
		{Key: 0xdeadbeef, Move: encode(4, 1, 4, 3, 0), Weight: 1}, // a different position
	})

	m, fromBook, err := BestMoveTimedWithBook(start, 2, time.Second, bk)
	if err != nil {
		t.Fatalf("BestMoveTimedWithBook: %v", err)
	}
	if fromBook {
		t.Error("fromBook = true, want false (position isn't in the book)")
	}
	if m.String() == "" {
		t.Error("expected a real move from the fallback timed search")
	}
}
