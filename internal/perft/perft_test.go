package perft

import (
	"testing"

	"github.com/yourname/dyanis-chess-engine/internal/board"
)

// These two tests exist specifically to catch bugs in MakeMove's
// incremental Zobrist hash maintenance (see internal/board/move.go) —
// not move generation itself, which is what the rest of this package
// already covers via Perft's reference-number comparisons. A missed
// XOR anywhere in MakeMove (en passant is the classic place to get
// this wrong, since it depends on both the old AND new board's pawn
// geometry) would silently corrupt the transposition table without
// ever showing up as a wrong move count, so this needs its own check.
//
// Depths are chosen to comfortably exercise every special-move case
// (castling both sides, en passant, all four promotion pieces, both
// plain and promotion captures) while still running in well under a
// second — raise them if you want more paranoia after touching
// MakeMove's hash logic again, at the cost of a slower test suite.

func TestIncrementalHashMatchesFromScratch_StartingPosition(t *testing.T) {
	b := board.NewInitialBoard()
	if fen := VerifyHashes(b, 4); fen != "" {
		t.Fatalf("incremental hash (MakeMove) disagrees with a from-scratch recomputation at: %s", fen)
	}
}

func TestIncrementalHashMatchesFromScratch_Kiwipete(t *testing.T) {
	// Kiwipete: the standard perft torture-test position, deliberately
	// packed with castling rights on both sides, an en passant
	// opportunity, and pieces poised for every kind of capture —
	// exactly the material this check needs to stress.
	const kiwipeteFEN = "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1"
	b, err := board.FromFEN(kiwipeteFEN)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}
	if fen := VerifyHashes(b, 3); fen != "" {
		t.Fatalf("incremental hash (MakeMove) disagrees with a from-scratch recomputation at: %s", fen)
	}
}

// The two tests below are VerifyHashes' exact counterpart for the
// incremental bitboard bookkeeping in MakeMove/UnmakeMove (see
// internal/board/move.go, internal/board/bitboard.go) — same reason
// to exist (a missed put/remove/move anywhere in MakeMove would
// silently corrupt board.Board.bb without changing any move count),
// same two positions, same depths.

func TestIncrementalBitboardsMatchFromScratch_StartingPosition(t *testing.T) {
	b := board.NewInitialBoard()
	if fen := VerifyBitboards(b, 4); fen != "" {
		t.Fatalf("incremental bitboards (MakeMove) disagree with a from-scratch recomputation at: %s", fen)
	}
}

func TestIncrementalBitboardsMatchFromScratch_Kiwipete(t *testing.T) {
	const kiwipeteFEN = "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1"
	b, err := board.FromFEN(kiwipeteFEN)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}
	if fen := VerifyBitboards(b, 3); fen != "" {
		t.Fatalf("incremental bitboards (MakeMove) disagree with a from-scratch recomputation at: %s", fen)
	}
}
