package movegen

import (
	"testing"

	"github.com/yourname/dyanis-chess-engine/internal/board"
)

func TestInitialPositionMoveCount(t *testing.T) {
	b := board.NewInitialBoard()
	moves := GenerateLegalMoves(b)
	// 16 pawn moves (8 single + 8 double) + 4 knight moves = 20.
	if len(moves) != 20 {
		t.Errorf("expected 20 legal moves from the start position, got %d", len(moves))
	}
}

func TestEnPassantCapture(t *testing.T) {
	b := board.NewInitialBoard()

	// 1. e4 e5? no - set up a real en passant scenario:
	// 1. e4 a6 2. e5 d5 (black double push next to white's e5 pawn)
	// then White should be able to play exd6 en passant.
	play := func(b *board.Board, from, to string) *board.Board {
		f, _ := board.ParseSquare(from)
		tt, _ := board.ParseSquare(to)
		for _, m := range GenerateLegalMoves(b) {
			if m.From == f && m.To == tt {
				return b.MakeMove(m)
			}
		}
		t.Fatalf("move %s-%s not found as legal", from, to)
		return nil
	}

	b = play(b, "e2", "e4")
	b = play(b, "a7", "a6")
	b = play(b, "e4", "e5")
	b = play(b, "d7", "d5") // black double push, sits beside White's e5 pawn

	epSq, _ := board.ParseSquare("d6")
	if b.EnPassant != epSq {
		t.Fatalf("expected en passant square d6, got %v", b.EnPassant)
	}

	found := false
	for _, m := range GenerateLegalMoves(b) {
		if m.Flag == board.EnPassantCapture {
			found = true
			if m.To != epSq {
				t.Errorf("expected en passant capture to d6, got %v", m.To)
			}
		}
	}
	if !found {
		t.Errorf("expected an en passant capture move to be generated")
	}
}

func TestCastlingAvailable(t *testing.T) {
	// Clear the back rank between king and rooks manually via a custom
	// board, since the initial position has no legal castling moves.
	b := board.NewInitialBoard()
	// Empty the squares between White king and both rooks: b1,c1,d1,f1,g1
	for _, s := range []string{"b1", "c1", "d1", "f1", "g1"} {
		sq, _ := board.ParseSquare(s)
		b.Squares[sq] = board.None
	}

	foundKingside, foundQueenside := false, false
	for _, m := range GenerateLegalMoves(b) {
		if m.Flag == board.CastleKingside {
			foundKingside = true
		}
		if m.Flag == board.CastleQueenside {
			foundQueenside = true
		}
	}
	if !foundKingside {
		t.Errorf("expected kingside castling to be available")
	}
	if !foundQueenside {
		t.Errorf("expected queenside castling to be available")
	}
}
