package movegen

import (
	"testing"

	"github.com/yourname/dyanis-chess-engine/internal/board"
)

// These two tests exist specifically to catch bugs in the pin-mask/
// checkers-mask legal-move generator (GenerateLegalMoves, movegen.go)
// replacing the make/unmake-filtered version (generateLegalMovesSlow)
// — not move generation's raw correctness in general, which the rest
// of the perft suite already covers via known reference counts. A bug
// here (a missed pin, a wrong checkMask, the en passant discovered-
// check case) could easily still produce the RIGHT total move COUNT
// at a given depth while generating the wrong SET of moves — sameMoveSet
// comparing the two generators directly at every node is what catches
// that; perft's leaf-count comparison alone would not.
//
// Same two positions and depths perft_test.go's hash/bitboard checks
// use, for the same reason: Kiwipete in particular is deliberately
// packed with castling rights on both sides, an en passant
// opportunity, and pieces poised for every kind of capture — exactly
// the material this check needs to stress (checks, pins, and en
// passant all appear naturally within a few plies of both positions).

func TestLegalMoveGenerationMatchesSlow_StartingPosition(t *testing.T) {
	b := board.NewInitialBoard()
	if fen := VerifyLegalMoveGeneration(b, 4); fen != "" {
		t.Fatalf("GenerateLegalMoves disagrees with generateLegalMovesSlow at: %s", fen)
	}
}

func TestLegalMoveGenerationMatchesSlow_Kiwipete(t *testing.T) {
	const kiwipeteFEN = "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1"
	b, err := board.FromFEN(kiwipeteFEN)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}
	if fen := VerifyLegalMoveGeneration(b, 3); fen != "" {
		t.Fatalf("GenerateLegalMoves disagrees with generateLegalMovesSlow at: %s", fen)
	}
}
