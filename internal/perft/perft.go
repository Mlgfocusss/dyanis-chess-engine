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
// over legal moves. depth 0 returns 1 (the position itself).
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
		nodes += Perft(b.MakeMove(m), depth-1)
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
		result[m.String()] = Perft(b.MakeMove(m), depth-1)
	}
	return result
}
