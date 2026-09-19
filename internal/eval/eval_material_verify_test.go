package eval

import (
	"testing"

	"github.com/yourname/dyanis-chess-engine/internal/board"
	"github.com/yourname/dyanis-chess-engine/internal/movegen"
)

// verifyMaterialPST walks the legal-move tree from b to the given
// depth and checks board.Board.VerifyMaterialPST() at every node —
// same "first offending FEN, or empty string" contract
// perft.VerifyHashes/VerifyBitboards use (see internal/perft/perft.go),
// adapted to live here instead: the incremental hook this checks
// (board.PieceValue, see material_hook.go / this package's own init()
// in pst.go) is only ever registered by importing this package, and
// perft doesn't — a walk there would just be comparing a permanently-
// nil hook against itself, trivially "passing" without exercising
// anything.
func verifyMaterialPST(b *board.Board, depth int) string {
	if !b.VerifyMaterialPST() {
		return b.ToFEN()
	}
	if depth == 0 {
		return ""
	}
	for _, m := range movegen.GenerateLegalMoves(b) {
		undo := b.MakeMove(m)
		fen := verifyMaterialPST(b, depth-1)
		b.UnmakeMove(m, undo)
		if fen != "" {
			return fen
		}
	}
	return ""
}

// These two tests exist specifically to catch bugs in MakeMove/
// UnmakeMove/SetSquare's incremental materialPST bookkeeping (see
// board/material_hook.go) — not material/PST scoring's own
// correctness, which the rest of this package's tests already cover
// through Evaluate(). Same two positions and depths the hash/bitboard/
// legal-move-generation checks elsewhere in this codebase already use,
// for the same reason: Kiwipete in particular is deliberately packed
// with castling rights, an en passant opportunity, and pieces poised
// for every kind of capture and promotion — exactly the material this
// needs to stress.
func TestIncrementalMaterialPSTMatchesFromScratch_StartingPosition(t *testing.T) {
	b := board.NewInitialBoard()
	if fen := verifyMaterialPST(b, 4); fen != "" {
		t.Fatalf("incremental MaterialPST (MakeMove) disagrees with a from-scratch recomputation at: %s", fen)
	}
}

func TestIncrementalMaterialPSTMatchesFromScratch_Kiwipete(t *testing.T) {
	const kiwipeteFEN = "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1"
	b, err := board.FromFEN(kiwipeteFEN)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}
	if fen := verifyMaterialPST(b, 3); fen != "" {
		t.Fatalf("incremental MaterialPST (MakeMove) disagrees with a from-scratch recomputation at: %s", fen)
	}
}
