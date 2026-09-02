// Opening-book integration for search's entry point. Kept in its own
// file, separate from search.go's actual negamax/alpha-beta code,
// since this isn't "search" in the tree-walking sense — it's a
// decision made *before* search runs at all.
package search

import (
	"math/rand"
	"time"

	"github.com/yourname/dyanis-chess-engine/internal/board"
	"github.com/yourname/dyanis-chess-engine/internal/book"
	"github.com/yourname/dyanis-chess-engine/internal/movegen"
)

// BestMoveWithBook is BestMove's book-aware entry point. If bk has one
// or more (nonzero-weight) moves recorded for the current position, it
// plays one of those instead of searching — book moves come from real
// games (by strong humans or engines, depending on the book), so for
// the handful of moves a book actually covers, that's a far better
// prior than what a fixed-depth, positionally shallow search can work
// out on its own.
//
// This deliberately does NOT feed book weight into eval.Evaluate.
// Evaluate scores arbitrary positions, including ones many plies deep
// inside the search tree that have nothing to do with the book and
// have no natural "how well-known is this" value a weight could
// represent — corrupting that scoring for the sake of one decision at
// the root would be a bad trade. So the book stays a short-circuit
// here, before search ever runs, exactly as book.go's original TODO
// called for.
//
// bk may be nil (no book loaded), in which case this is identical to
// calling BestMove directly. The bool return reports whether the move
// came from the book, purely so callers (e.g. the CLI) can say so.
func BestMoveWithBook(b *board.Board, depth int, bk *book.Book) (m board.Move, fromBook bool, err error) {
	if bk != nil {
		if bm, ok := pickBookMove(b, bk); ok {
			return bm, true, nil
		}
	}
	m, err = BestMove(b, depth)
	return m, false, err
}

// BestMoveTimedWithBook is BestMoveTimed's book-aware counterpart,
// with the exact same book-first reasoning as BestMoveWithBook — see
// its comment for why the book is checked here rather than folded
// into eval. maxDepth/budget are passed straight through to
// BestMoveTimed when there's no book hit.
func BestMoveTimedWithBook(b *board.Board, maxDepth int, budget time.Duration, bk *book.Book) (m board.Move, fromBook bool, err error) {
	if bk != nil {
		if bm, ok := pickBookMove(b, bk); ok {
			return bm, true, nil
		}
	}
	m, err = BestMoveTimed(b, maxDepth, budget)
	return m, false, err
}

// pickBookMove looks up b in the book and, among entries with nonzero
// weight (Polyglot's convention: weight 0 means "deleted", so those
// are skipped), picks one at random weighted by Entry.Weight. A move
// played in 10,000 recorded games is picked far more often than one
// played in 3, but the engine won't always play the single most
// popular try — always doing that would make it perfectly predictable
// and throw away the variety the weights are meant to encode.
func pickBookMove(b *board.Board, bk *book.Book) (board.Move, bool) {
	entries := bk.LookupBoard(b)
	if len(entries) == 0 {
		return board.Move{}, false
	}

	total := 0
	for _, e := range entries {
		total += int(e.Weight)
	}
	if total == 0 {
		return board.Move{}, false
	}

	legal := movegen.GenerateLegalMoves(b)

	pick := rand.Intn(total)
	sum := 0
	for _, e := range entries {
		sum += int(e.Weight)
		if pick < sum {
			return book.DecodeMove(e, legal)
		}
	}
	return board.Move{}, false // unreachable given total > 0
}
