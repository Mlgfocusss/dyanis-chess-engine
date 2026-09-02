package search

import (
	"testing"
	"time"

	"github.com/yourname/dyanis-chess-engine/internal/board"
	"github.com/yourname/dyanis-chess-engine/internal/movegen"
)

func TestFindsMateInOne(t *testing.T) {
	// Classic back-rank mate pattern: Black king trapped on g8 by its
	// own pawns, White rook delivers mate by moving to the back rank.
	const fen = "6k1/5ppp/8/8/8/8/8/R5K1 w - - 0 1"
	b, err := board.FromFEN(fen)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}

	m, err := BestMove(b, 1)
	if err != nil {
		t.Fatalf("BestMove failed: %v", err)
	}

	want := "a1a8"
	if m.String() != want {
		t.Errorf("expected mate-in-1 move %s, got %s", want, m)
	}
}

func TestPrefersWinningMaterial(t *testing.T) {
	// White queen can capture a hanging Black rook on d8 for free
	// (no Black piece defends or recaptures it). At depth 2 the
	// engine should prefer grabbing the rook over any quiet move.
	const fen = "3r2k1/8/8/8/8/8/8/3Q2K1 w - - 0 1"
	b, err := board.FromFEN(fen)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}

	m, err := BestMove(b, 2)
	if err != nil {
		t.Fatalf("BestMove failed: %v", err)
	}

	want := "d1d8"
	if m.String() != want {
		t.Errorf("expected the engine to capture the hanging rook with %s, got %s", want, m)
	}
}

func TestMVVLVARanksPawnTakesQueenAboveQueenTakesPawn(t *testing.T) {
	// White has two ways to capture on d5: a pawn from c4, or a queen
	// from d1. Taking with the pawn should score higher than taking
	// with the queen — MVV-LVA doesn't care that both captures land
	// on the same square and take the same piece, only "what did the
	// capturing" differs, and a pawn is a much less valuable piece to
	// commit than a queen.
	const fen = "4k3/8/8/3p4/2P5/8/8/3QK3 w - - 0 1"
	b, err := board.FromFEN(fen)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}

	pawnTakes := findMove(t, b, "c4d5")
	queenTakes := findMove(t, b, "d1d5")

	pawnScore := mvvLva(b, pawnTakes)
	queenScore := mvvLva(b, queenTakes)

	if pawnScore <= queenScore {
		t.Errorf("expected pawn-takes-pawn (%d) to outrank queen-takes-pawn (%d) in MVV-LVA ordering", pawnScore, queenScore)
	}
}

func TestKillerRecordedOnBetaCutoff(t *testing.T) {
	// Not testing a specific position's tactics here, just the
	// bookkeeping: after a search that's deep enough to produce at
	// least one beta cutoff on a quiet move, the table should have
	// recorded a killer move at some depth. The starting position at
	// a shallow depth reliably produces cutoffs in practice (plenty
	// of roughly-equal quiet moves for alpha-beta to prune between).
	b := board.NewInitialBoard()
	tt := NewTranspositionTable()
	negamax(b, 3, -Infinity, Infinity, true, tt)

	found := false
	for _, slots := range tt.killers {
		if slots[0] != (board.Move{}) || slots[1] != (board.Move{}) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one killer move to be recorded after searching the starting position to depth 3")
	}
}

func TestOrderingDoesNotBreakMateSearch(t *testing.T) {
	// Regression check: the new move-ordering machinery (TT move,
	// MVV-LVA, killers, history) must never change WHICH move is
	// correct, only the order candidates are tried in. Re-run the
	// mate-in-1 case through the timed/iterative-deepening entry
	// point too, since that's a second, separately-tested code path
	// that also calls negamax with the same TranspositionTable.
	const fen = "6k1/5ppp/8/8/8/8/8/R5K1 w - - 0 1"
	b, err := board.FromFEN(fen)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}

	m, err := BestMoveTimed(b, 3, time.Second)
	if err != nil {
		t.Fatalf("BestMoveTimed failed: %v", err)
	}

	want := "a1a8"
	if m.String() != want {
		t.Errorf("expected mate-in-1 move %s, got %s", want, m)
	}
}

func TestHasNonPawnMaterialGuardsZugzwang(t *testing.T) {
	// Only kings and pawns: null-move pruning's zugzwang guard should
	// refuse to consider this position eligible.
	const kpFen = "8/8/8/4k3/4P3/4K3/8/8 w - - 0 1"
	b, err := board.FromFEN(kpFen)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}
	if hasNonPawnMaterial(b, board.White) {
		t.Error("expected hasNonPawnMaterial to be false with only king+pawn material")
	}

	// Add a white knight: now there's non-pawn material, and NMP is
	// allowed to consider a null-move probe for White.
	const withKnightFen = "8/8/8/4k3/4P3/4KN2/8/8 w - - 0 1"
	b2, err := board.FromFEN(withKnightFen)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}
	if !hasNonPawnMaterial(b2, board.White) {
		t.Error("expected hasNonPawnMaterial to be true once a knight is on the board")
	}
}

func TestNullMovePruningKeepsCorrectAnswerAtDepth(t *testing.T) {
	// Depth 4 is deep enough (>= nullMoveMinDepth, with room for the
	// reduced probe below it) to actually exercise the null-move
	// pruning branch in negamax during this search, not just its
	// guards in isolation. This checks NMP doesn't cause the engine
	// to miss a simple hanging-material capture once it's eligible to
	// fire — same position as TestPrefersWinningMaterial, deeper.
	const fen = "3r2k1/8/8/8/8/8/8/3Q2K1 w - - 0 1"
	b, err := board.FromFEN(fen)
	if err != nil {
		t.Fatalf("FEN parse failed: %v", err)
	}

	m, err := BestMove(b, 4)
	if err != nil {
		t.Fatalf("BestMove failed: %v", err)
	}

	want := "d1d8"
	if m.String() != want {
		t.Errorf("expected the engine to still capture the hanging rook with %s at depth 4, got %s", want, m)
	}
}

// findMove locates a legal move by its coordinate-notation string
// (e.g. "e2e4"), failing the test if it isn't legal in b — a small
// helper so ordering tests can name moves the same way a person would
// rather than constructing board.Move values by hand.
func findMove(t *testing.T, b *board.Board, uci string) board.Move {
	t.Helper()
	for _, m := range movegen.GenerateLegalMoves(b) {
		if m.String() == uci {
			return m
		}
	}
	t.Fatalf("move %q is not legal in this position", uci)
	return board.Move{}
}
