package board

import "testing"

func TestInitialPosition(t *testing.T) {
	b := NewInitialBoard()

	if b.SideToMove != White {
		t.Errorf("expected White to move first, got %v", b.SideToMove)
	}

	if b.Squares[MakeSquare(4, 0)] != WK {
		t.Errorf("expected White king on e1, got %v", b.Squares[MakeSquare(4, 0)])
	}
	if b.Squares[MakeSquare(4, 7)] != BK {
		t.Errorf("expected Black king on e8, got %v", b.Squares[MakeSquare(4, 7)])
	}

	for file := 0; file < 8; file++ {
		if b.Squares[MakeSquare(file, 1)] != WP {
			t.Errorf("expected White pawn on file %d rank 2", file)
		}
		if b.Squares[MakeSquare(file, 6)] != BP {
			t.Errorf("expected Black pawn on file %d rank 7", file)
		}
	}

	want := WhiteKingside | WhiteQueenside | BlackKingside | BlackQueenside
	if b.Castling != want {
		t.Errorf("expected full castling rights, got %v", b.Castling)
	}

	if b.EnPassant != NoSquare {
		t.Errorf("expected no en passant square at start, got %v", b.EnPassant)
	}
}

func TestSquareRoundTrip(t *testing.T) {
	cases := []string{"a1", "h1", "a8", "h8", "e4", "d5"}
	for _, s := range cases {
		sq, err := ParseSquare(s)
		if err != nil {
			t.Fatalf("ParseSquare(%q) failed: %v", s, err)
		}
		if sq.String() != s {
			t.Errorf("round trip failed: %q -> %v -> %q", s, sq, sq.String())
		}
	}
}

func TestKingSquare(t *testing.T) {
	b := NewInitialBoard()
	e1, _ := ParseSquare("e1")
	e8, _ := ParseSquare("e8")
	if b.KingSquare(White) != e1 {
		t.Errorf("expected White king square e1, got %v", b.KingSquare(White))
	}
	if b.KingSquare(Black) != e8 {
		t.Errorf("expected Black king square e8, got %v", b.KingSquare(Black))
	}
}
