package book

import (
	"testing"

	"github.com/yourname/dyanis-chess-engine/internal/board"
	"github.com/yourname/dyanis-chess-engine/internal/movegen"
)

// encode mirrors Polyglot's 16-bit move packing, for building synthetic
// test entries without hand-computing bit patterns.
func encode(fromFile, fromRank, toFile, toRank int, promo uint16) uint16 {
	return uint16(toFile) | uint16(toRank)<<3 | uint16(fromFile)<<6 | uint16(fromRank)<<9 | promo<<12
}

func TestDecodeRawQuietMove(t *testing.T) {
	raw := encode(4, 1, 4, 3, 0) // e2 -> e4, no promotion
	from, to, promo := decodeRaw(raw)

	wantFrom, _ := board.ParseSquare("e2")
	wantTo, _ := board.ParseSquare("e4")
	if from != wantFrom || to != wantTo {
		t.Errorf("decodeRaw = (%v, %v), want (%v, %v)", from, to, wantFrom, wantTo)
	}
	if promo != board.NoType {
		t.Errorf("promotion = %v, want NoType", promo)
	}
}

func TestDecodeRawPromotion(t *testing.T) {
	raw := encode(6, 6, 6, 7, 4) // g7 -> g8, promo bits = 4 (queen)
	_, _, promo := decodeRaw(raw)
	if promo != board.Queen {
		t.Errorf("promotion = %v, want Queen", promo)
	}
}

func TestNormalizeCastling(t *testing.T) {
	cases := []struct {
		name         string
		fromAlg      string
		toAlg        string
		wantToAlg    string
		isCastleCase bool
	}{
		{"white kingside", "e1", "h1", "g1", true},
		{"white queenside", "e1", "a1", "c1", true},
		{"black kingside", "e8", "h8", "g8", true},
		{"black queenside", "e8", "a8", "c8", true},
		{"ordinary move untouched", "e2", "e4", "e4", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			from, _ := board.ParseSquare(c.fromAlg)
			to, _ := board.ParseSquare(c.toAlg)
			wantTo, _ := board.ParseSquare(c.wantToAlg)

			got := normalizeCastling(from, to)
			if got != wantTo {
				t.Errorf("normalizeCastling(%s,%s) = %v, want %v", c.fromAlg, c.toAlg, got, wantTo)
			}
		})
	}
}

func TestDecodeMoveQuiet(t *testing.T) {
	b := board.NewInitialBoard()
	legal := movegen.GenerateLegalMoves(b)

	entry := Entry{Move: encode(4, 1, 4, 3, 0)} // e2e4
	m, ok := DecodeMove(entry, legal)
	if !ok {
		t.Fatal("DecodeMove: expected a match for e2e4 in the starting position")
	}
	if m.String() != "e2e4" {
		t.Errorf("decoded move = %q, want %q", m.String(), "e2e4")
	}
}

func TestDecodeMoveCastling(t *testing.T) {
	// White king on e1, rook on h1, nothing in the way, no checks:
	// kingside castling is legal.
	b, err := board.FromFEN("4k3/8/8/8/8/8/8/4K2R w K - 0 1")
	if err != nil {
		t.Fatalf("FromFEN: %v", err)
	}
	legal := movegen.GenerateLegalMoves(b)

	// Polyglot encodes this as "king takes own rook": e1h1.
	entry := Entry{Move: encode(4, 0, 7, 0, 0)}
	m, ok := DecodeMove(entry, legal)
	if !ok {
		t.Fatal("DecodeMove: expected a match for the e1h1-encoded castling move")
	}
	if m.Flag != board.CastleKingside {
		t.Errorf("decoded move flag = %v, want CastleKingside", m.Flag)
	}
	if m.String() != "e1g1" {
		t.Errorf("decoded move = %q, want %q", m.String(), "e1g1")
	}
}

func TestDecodeMoveNoMatch(t *testing.T) {
	b := board.NewInitialBoard()
	legal := movegen.GenerateLegalMoves(b)

	// e2e5 isn't a legal move from the starting position.
	entry := Entry{Move: encode(4, 1, 4, 4, 0)}
	if _, ok := DecodeMove(entry, legal); ok {
		t.Error("DecodeMove: expected no match for an illegal move")
	}
}
