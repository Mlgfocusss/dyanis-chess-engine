package movegen

import (
	"testing"

	"github.com/yourname/dyanis-chess-engine/internal/board"
)

func mustSquare(t *testing.T, alg string) board.Square {
	t.Helper()
	sq, err := board.ParseSquare(alg)
	if err != nil {
		t.Fatalf("ParseSquare(%q): %v", alg, err)
	}
	return sq
}

func TestSANPawnPush(t *testing.T) {
	b := board.NewInitialBoard()
	e2, e4 := mustSquare(t, "e2"), mustSquare(t, "e4")
	m := board.Move{From: e2, To: e4, Flag: board.DoublePawnPush}

	if got := SAN(b, m); got != "e4" {
		t.Errorf("SAN = %q, want %q", got, "e4")
	}
}

func TestSANKnightDisambiguation(t *testing.T) {
	// White knights on b3 and f3 both attack d2.
	b, err := board.FromFEN("4k3/8/8/8/8/1N3N2/8/4K3 w - - 0 1")
	if err != nil {
		t.Fatalf("FromFEN: %v", err)
	}
	b3, f3, d2 := mustSquare(t, "b3"), mustSquare(t, "f3"), mustSquare(t, "d2")

	if got := SAN(b, board.Move{From: b3, To: d2, Flag: board.Quiet}); got != "Nbd2" {
		t.Errorf("SAN(b3d2) = %q, want %q", got, "Nbd2")
	}
	if got := SAN(b, board.Move{From: f3, To: d2, Flag: board.Quiet}); got != "Nfd2" {
		t.Errorf("SAN(f3d2) = %q, want %q", got, "Nfd2")
	}
}

func TestSANCastlingKingside(t *testing.T) {
	b, err := board.FromFEN("4k3/8/8/8/8/8/8/4K2R w K - 0 1")
	if err != nil {
		t.Fatalf("FromFEN: %v", err)
	}
	e1, g1 := mustSquare(t, "e1"), mustSquare(t, "g1")

	if got := SAN(b, board.Move{From: e1, To: g1, Flag: board.CastleKingside}); got != "O-O" {
		t.Errorf("SAN(castle) = %q, want %q", got, "O-O")
	}
}

func TestSANPromotionNoCheck(t *testing.T) {
	b, err := board.FromFEN("8/1P6/8/7k/8/8/8/4K3 w - - 0 1")
	if err != nil {
		t.Fatalf("FromFEN: %v", err)
	}
	b7, b8 := mustSquare(t, "b7"), mustSquare(t, "b8")
	m := board.Move{From: b7, To: b8, Flag: board.Promotion, Promotion: board.Queen}

	if got := SAN(b, m); got != "b8=Q" {
		t.Errorf("SAN(promotion) = %q, want %q", got, "b8=Q")
	}
}

func TestSANCheckmate(t *testing.T) {
	// White rook a1-a8: black king g8 is boxed in by its own pawns on
	// f7/g7/h7 and nothing can block or capture on the back rank.
	b, err := board.FromFEN("6k1/5ppp/8/8/8/8/8/R7 w - - 0 1")
	if err != nil {
		t.Fatalf("FromFEN: %v", err)
	}
	a1, a8 := mustSquare(t, "a1"), mustSquare(t, "a8")
	m := board.Move{From: a1, To: a8, Flag: board.Quiet}

	if got := SAN(b, m); got != "Ra8#" {
		t.Errorf("SAN(mate) = %q, want %q", got, "Ra8#")
	}
}

func TestParseSANPawnPush(t *testing.T) {
	b := board.NewInitialBoard()
	m, err := ParseSAN(b, "e4")
	if err != nil {
		t.Fatalf("ParseSAN: %v", err)
	}
	if m.String() != "e2e4" {
		t.Errorf("ParseSAN(e4) = %v, want e2e4", m)
	}
}

func TestParseSANKnightMove(t *testing.T) {
	b := board.NewInitialBoard()
	m, err := ParseSAN(b, "Nf3")
	if err != nil {
		t.Fatalf("ParseSAN: %v", err)
	}
	if m.String() != "g1f3" {
		t.Errorf("ParseSAN(Nf3) = %v, want g1f3", m)
	}
}

func TestParseSANDisambiguation(t *testing.T) {
	b, err := board.FromFEN("4k3/8/8/8/8/1N3N2/8/4K3 w - - 0 1")
	if err != nil {
		t.Fatalf("FromFEN: %v", err)
	}

	m, err := ParseSAN(b, "Nbd2")
	if err != nil {
		t.Fatalf("ParseSAN(Nbd2): %v", err)
	}
	if m.String() != "b3d2" {
		t.Errorf("ParseSAN(Nbd2) = %v, want b3d2", m)
	}

	if _, err := ParseSAN(b, "Nd2"); err == nil {
		t.Error("ParseSAN(Nd2) should be ambiguous (two knights can reach d2)")
	}
}

func TestParseSANCastling(t *testing.T) {
	b, err := board.FromFEN("4k3/8/8/8/8/8/8/4K2R w K - 0 1")
	if err != nil {
		t.Fatalf("FromFEN: %v", err)
	}
	m, err := ParseSAN(b, "O-O")
	if err != nil {
		t.Fatalf("ParseSAN(O-O): %v", err)
	}
	if m.Flag != board.CastleKingside {
		t.Errorf("ParseSAN(O-O) flag = %v, want CastleKingside", m.Flag)
	}
}

func TestParseSANPromotionDefaultsToQueen(t *testing.T) {
	b, err := board.FromFEN("8/1P6/8/7k/8/8/8/4K3 w - - 0 1")
	if err != nil {
		t.Fatalf("FromFEN: %v", err)
	}
	m, err := ParseSAN(b, "b8")
	if err != nil {
		t.Fatalf("ParseSAN(b8): %v", err)
	}
	if m.Promotion != board.Queen {
		t.Errorf("ParseSAN(b8) promotion = %v, want Queen (default when suffix omitted)", m.Promotion)
	}

	m2, err := ParseSAN(b, "b8=Q")
	if err != nil {
		t.Fatalf("ParseSAN(b8=Q): %v", err)
	}
	if m2 != m {
		t.Errorf("ParseSAN(b8) and ParseSAN(b8=Q) should produce the same move")
	}
}

func TestSANAndParseSANRoundTrip(t *testing.T) {
	// Every legal move from the starting position: SAN it, then parse
	// that SAN back, and check we land on the exact same move.
	b := board.NewInitialBoard()
	for _, m := range GenerateLegalMoves(b) {
		san := SAN(b, m)
		parsed, err := ParseSAN(b, san)
		if err != nil {
			t.Errorf("ParseSAN(%q): %v", san, err)
			continue
		}
		if parsed != m {
			t.Errorf("round trip for %v: SAN = %q, ParseSAN back = %v", m, san, parsed)
		}
	}
}
