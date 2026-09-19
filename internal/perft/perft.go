// Package perft implements the standard "performance test" used to
// validate a move generator: count the number of leaf nodes reachable
// from a position at a fixed depth, and compare against known-correct
// reference numbers. If move generation has a bug (missed en passant,
// wrong castling rights, illegal move slipping through, legal move
// wrongly excluded), the counts diverge from the reference values,
// usually starting at a specific depth that hints at the bug's cause.
package perft

import (
	"github.com/yourname/dyanis-chess-engine/internal/board"
	"github.com/yourname/dyanis-chess-engine/internal/movegen"
)

// Perft counts leaf nodes at the given depth by brute-force recursion
// over legal moves. depth 0 returns 1 (the position itself). Walks
// via make/unmake on b itself: b is back to exactly how it started by
// the time this returns, so callers can keep using the same board
// they passed in.
func Perft(b *board.Board, depth int) uint64 {
	if depth == 0 {
		return 1
	}
	moves := movegen.GenerateLegalMoves(b)
	if depth == 1 {
		return uint64(len(moves))
	}

	var nodes uint64
	for _, m := range moves {
		undo := b.MakeMove(m)
		nodes += Perft(b, depth-1)
		b.UnmakeMove(m, undo)
	}
	return nodes
}

// Divide is like Perft but breaks the count down per root move, which
// is the standard technique for tracking down where a move generator
// disagrees with the reference numbers: run Divide at the shallowest
// depth where the total is wrong, find the one root move whose subtree
// count is off, and recurse into it.
func Divide(b *board.Board, depth int) map[string]uint64 {
	result := make(map[string]uint64)
	if depth < 1 {
		return result
	}
	for _, m := range movegen.GenerateLegalMoves(b) {
		undo := b.MakeMove(m)
		result[m.String()] = Perft(b, depth-1)
		b.UnmakeMove(m, undo)
	}
	return result
}

// VerifyHashes walks the exact same legal-move tree Perft does, and
// at every single node — not just the leaves — checks
// board.Board.VerifyHash(): that MakeMove's incrementally-maintained
// Zobrist hash (see internal/board/move.go) still agrees with a full
// from-scratch recomputation. Returns the FEN of the first position
// where they disagree, or "" if none is found anywhere in the tree.
//
// This deliberately piggybacks on the same recursive walk Perft/
// Divide already use, rather than a second, separate traversal:
// perft's whole point is exercising every kind of move (captures,
// castling both sides, promotions, en passant) at real game
// positions, which is exactly the coverage an incremental-hash bug
// needs to be caught by — building a second parallel traversal just
// for hash-checking would only be a second place for the two to
// quietly drift out of sync with each other.
func VerifyHashes(b *board.Board, depth int) string {
	if !b.VerifyHash() {
		return b.ToFEN()
	}
	if depth == 0 {
		return ""
	}
	for _, m := range movegen.GenerateLegalMoves(b) {
		undo := b.MakeMove(m)
		fen := VerifyHashes(b, depth-1)
		b.UnmakeMove(m, undo)
		if fen != "" {
			return fen
		}
	}
	return ""
}

// VerifyBitboards is VerifyHashes' exact counterpart for the
// incremental bitboard bookkeeping added to MakeMove/UnmakeMove
// alongside the hash (see internal/board/move.go, internal/board/
// bitboard.go): same recursive walk over the legal-move tree, same
// per-node check (board.Board.VerifyBitboards() instead of
// VerifyHash()), same "first offending FEN, or empty string if none"
// contract. Deliberately a second, separate top-to-bottom walk rather
// than folding this check into VerifyHashes' existing traversal — the
// two are checking unrelated pieces of incremental state (Zobrist hash
// vs. bitboard snapshot) maintained by different lines of MakeMove,
// and keeping them as two independent walks means a bug that trips
// one doesn't obscure whether the other is also broken at the same
// node.
func VerifyBitboards(b *board.Board, depth int) string {
	if !b.VerifyBitboards() {
		return b.ToFEN()
	}
	if depth == 0 {
		return ""
	}
	for _, m := range movegen.GenerateLegalMoves(b) {
		undo := b.MakeMove(m)
		fen := VerifyBitboards(b, depth-1)
		b.UnmakeMove(m, undo)
		if fen != "" {
			return fen
		}
	}
	return ""
}
