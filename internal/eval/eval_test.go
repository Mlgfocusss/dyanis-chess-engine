// Same package as the rest of eval (not eval_test): tests here reach
// into unexported helpers, and the integration-level tests exercise
// Evaluate() end to end. Unit tests for each individual term live
// next to that term: pst_test.go, mobility_test.go, pawns_test.go,
// king_safety_test.go, bishops_test.go.
package eval

import (
	"testing"

	"github.com/yourname/dyanis-chess-engine/internal/board"
)

func sq(t *testing.T, alg string) board.Square {
	t.Helper()
	s, err := board.ParseSquare(alg)
	if err != nil {
		t.Fatalf("ParseSquare(%q): %v", alg, err)
	}
	return s
}

// emptyBoard returns a Board with no pieces on it at all, for tests
// that want to place only the handful of pieces relevant to what
// they're checking. Evaluate/its terms don't require a legal or even
// complete position (no check for missing kings, etc.), which is what
// makes this useful.
func emptyBoard() *board.Board {
	return &board.Board{SideToMove: board.White, EnPassant: board.NoSquare}
}

func TestEvaluateStartingPositionIsZero(t *testing.T) {
	b := board.NewInitialBoard()
	got := Evaluate(b, nil)
	if got != 0 {
		t.Errorf("Evaluate(start pos) = %d, want 0 (symmetric position)", got)
	}
}

func TestGamePhaseFullMaterialIsMax(t *testing.T) {
	b := board.NewInitialBoard()
	got := gamePhase(b)
	if got != maxPhase {
		t.Errorf("gamePhase(start pos) = %d, want %d", got, maxPhase)
	}
}

func TestGamePhaseNoMinorMajorPiecesIsZero(t *testing.T) {
	b := emptyBoard()
	b.SetSquare(sq(t, "e1"), board.WK)
	b.SetSquare(sq(t, "e8"), board.BK)
	got := gamePhase(b)
	if got != 0 {
		t.Errorf("gamePhase(kings only) = %d, want 0", got)
	}
}

func TestKingEndgamePrefersCenterOverCorner(t *testing.T) {
	// Only kings on the board -> phase 0 -> pure endgame table, and
	// mobility/pawn-structure/king-safety are all silent (no pawns, no
	// minor/major pieces).
	center := emptyBoard()
	center.SetSquare(sq(t, "a8"), board.BK)
	center.SetSquare(sq(t, "e4"), board.WK)

	corner := emptyBoard()
	corner.SetSquare(sq(t, "a8"), board.BK)
	corner.SetSquare(sq(t, "a1"), board.WK)

	if Evaluate(center, nil) <= Evaluate(corner, nil) {
		t.Errorf("in a bare-kings endgame, king on e4 (%d) should score higher than king on a1 (%d)",
			Evaluate(center, nil), Evaluate(corner, nil))
	}
}

func TestEvaluateFlipsSignForBlackToMove(t *testing.T) {
	// White is up a pawn. The score should flip sign (same magnitude)
	// depending purely on whose move it is.
	b := emptyBoard()
	b.SetSquare(sq(t, "e1"), board.WK)
	b.SetSquare(sq(t, "e8"), board.BK)
	b.SetSquare(sq(t, "a2"), board.WP)

	b.SideToMove = board.White
	white := Evaluate(b, nil)

	b.SideToMove = board.Black
	black := Evaluate(b, nil)

	if white != -black {
		t.Errorf("Evaluate() White-to-move=%d, Black-to-move=%d, want equal magnitude and opposite sign", white, black)
	}
	if white <= 0 {
		t.Errorf("Evaluate() with White up a pawn, White to move = %d, want > 0", white)
	}
}
