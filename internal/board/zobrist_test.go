package board

import "testing"

// TestHashMatchesKnownPolyglotConstant checks the transcribed 781-entry
// random table and the hashing logic together against a widely-cited
// external fact: the Polyglot Zobrist hash of the standard starting
// position is 0x463b96181691fc9c. If this fails, don't bother
// debugging move-by-move — it means the table itself has a typo
// somewhere and every downloaded .bin book will silently produce zero
// hits.
func TestHashMatchesKnownPolyglotConstant(t *testing.T) {
	b := NewInitialBoard()
	const want = uint64(0x463b96181691fc9c)
	if got := b.Hash(); got != want {
		t.Errorf("Hash(start pos) = %#x, want %#x", got, want)
	}
}

func TestHashChangesAfterAMove(t *testing.T) {
	before := NewInitialBoard()
	e2, _ := ParseSquare("e2")
	e4, _ := ParseSquare("e4")
	after := before.MakeMove(Move{From: e2, To: e4, Flag: DoublePawnPush})

	if before.Hash() == after.Hash() {
		t.Error("Hash() should differ between the starting position and after 1.e4")
	}
}

// TestEnPassantOnlyHashedWhenCapturable exercises the one genuinely
// tricky part of the Polyglot scheme: the en passant file is only
// XORed in if an enemy... actually a *friendly* (side-to-move) pawn is
// actually positioned to make the capture. A two-square pawn push
// with no adjacent enemy pawn must hash identically to the same
// position with EnPassant cleared entirely.
func TestEnPassantOnlyHashedWhenCapturable(t *testing.T) {
	e6, _ := ParseSquare("e6") // en passant target square
	d5, _ := ParseSquare("d5") // a white pawn here CAN capture on e6

	base := func() *Board {
		b := &Board{SideToMove: White, EnPassant: NoSquare}
		b.Squares[MakeSquare(4, 6)] = BK // arbitrary kings so the position isn't totally bare
		b.Squares[MakeSquare(4, 0)] = WK
		b.Squares[MakeSquare(4, 4)] = BP // the pawn that just double-pushed, sits on e5
		return b
	}

	noCapturer := base()
	noCapturer.EnPassant = e6

	withoutEPAtAll := base()
	withoutEPAtAll.EnPassant = NoSquare

	if noCapturer.Hash() != withoutEPAtAll.Hash() {
		t.Error("en passant square set but no pawn can capture: hash should match EnPassant=NoSquare")
	}

	withCapturer := base()
	withCapturer.EnPassant = e6
	withCapturer.Squares[d5] = WP

	if withCapturer.Hash() == withoutEPAtAll.Hash() {
		t.Error("en passant square set with a pawn actually able to capture: hash should differ from EnPassant=NoSquare")
	}
}
