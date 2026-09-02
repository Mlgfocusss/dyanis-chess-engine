// Package board defines the core chess board representation.
//
// Representation choice: a flat [64]Piece array, index = rank*8 + file,
// where file 0 = 'a', rank 0 = rank "1" (White's back rank).
// This is the simple 8x8 approach mentioned in the project plan (step 1).
// Bitboards are a possible future optimization; the rest of the codebase
// (movegen, search, eval) should only depend on the public API below,
// not on the fact that squares are stored in a flat array, so swapping
// the internals later doesn't require rewriting move generation logic.
package board

import "fmt"

// Color identifies a side.
type Color int8

const (
	White Color = iota
	Black
)

// Opposite returns the other color.
func (c Color) Opposite() Color {
	if c == White {
		return Black
	}
	return White
}

func (c Color) String() string {
	if c == White {
		return "white"
	}
	return "black"
}

// Piece encodes both color and piece type in a single signed value:
// positive = White, negative = Black, 0 = empty square.
// Absolute value 1..6 = Pawn..King (see constants below).
type Piece int8

const (
	None Piece = 0

	WP Piece = 1
	WN Piece = 2
	WB Piece = 3
	WR Piece = 4
	WQ Piece = 5
	WK Piece = 6

	BP Piece = -1
	BN Piece = -2
	BB Piece = -3
	BR Piece = -4
	BQ Piece = -5
	BK Piece = -6
)

// PieceType is the color-independent piece kind, used e.g. for
// promotion targets and for comparing piece kinds regardless of side.
type PieceType int8

const (
	NoType PieceType = 0
	Pawn   PieceType = 1
	Knight PieceType = 2
	Bishop PieceType = 3
	Rook   PieceType = 4
	Queen  PieceType = 5
	King   PieceType = 6
)

// Type returns the color-independent type of a piece.
func (p Piece) Type() PieceType {
	if p < 0 {
		return PieceType(-p)
	}
	return PieceType(p)
}

// Color returns the piece's color. Only valid if p != None.
func (p Piece) Color() Color {
	if p < 0 {
		return Black
	}
	return White
}

// IsNone reports whether the square is empty.
func (p Piece) IsNone() bool {
	return p == None
}

// MakePiece builds a Piece from a type and color.
func MakePiece(t PieceType, c Color) Piece {
	if c == White {
		return Piece(t)
	}
	return -Piece(t)
}

func (p Piece) String() string {
	letters := map[PieceType]string{
		Pawn: "p", Knight: "n", Bishop: "b", Rook: "r", Queen: "q", King: "k",
	}
	if p == None {
		return "."
	}
	s := letters[p.Type()]
	if p.Color() == White {
		return string(s[0] - 32) // uppercase for White
	}
	return s
}

// Square is a board index in [0, 63]. 0 = a1, 63 = h8.
type Square int8

const NoSquare Square = -1

func MakeSquare(file, rank int) Square {
	return Square(rank*8 + file)
}

func (s Square) File() int { return int(s) % 8 }
func (s Square) Rank() int { return int(s) / 8 }

func (s Square) String() string {
	if s == NoSquare {
		return "-"
	}
	f := "abcdefgh"[s.File()]
	r := "12345678"[s.Rank()]
	return fmt.Sprintf("%c%c", f, r)
}

// ParseSquare converts algebraic notation like "e4" into a Square.
func ParseSquare(s string) (Square, error) {
	if len(s) != 2 {
		return NoSquare, fmt.Errorf("invalid square: %q", s)
	}
	file := int(s[0] - 'a')
	rank := int(s[1] - '1')
	if file < 0 || file > 7 || rank < 0 || rank > 7 {
		return NoSquare, fmt.Errorf("invalid square: %q", s)
	}
	return MakeSquare(file, rank), nil
}

// Castling rights, stored as a bitmask.
type CastlingRights uint8

const (
	WhiteKingside CastlingRights = 1 << iota
	WhiteQueenside
	BlackKingside
	BlackQueenside
)

// Board is the full state needed to make/unmake moves and to resume
// play from any position (this mirrors what a FEN string encodes).
type Board struct {
	Squares [64]Piece

	SideToMove Color

	Castling CastlingRights

	// EnPassant is the square a pawn can capture on-the-fly to, i.e. the
	// square "behind" a pawn that just made a two-square advance.
	// NoSquare if no en passant capture is currently available.
	EnPassant Square

	HalfmoveClock  int // for the 50-move rule
	FullmoveNumber int
}

// NewInitialBoard returns the standard starting position.
func NewInitialBoard() *Board {
	b := &Board{
		SideToMove:     White,
		Castling:       WhiteKingside | WhiteQueenside | BlackKingside | BlackQueenside,
		EnPassant:      NoSquare,
		HalfmoveClock:  0,
		FullmoveNumber: 1,
	}

	backRank := []PieceType{Rook, Knight, Bishop, Queen, King, Bishop, Knight, Rook}
	for file := 0; file < 8; file++ {
		b.Squares[MakeSquare(file, 0)] = MakePiece(backRank[file], White)
		b.Squares[MakeSquare(file, 1)] = MakePiece(Pawn, White)
		b.Squares[MakeSquare(file, 6)] = MakePiece(Pawn, Black)
		b.Squares[MakeSquare(file, 7)] = MakePiece(backRank[file], Black)
	}
	return b
}

// Copy returns a deep copy of the board. Move generation works on
// copies for now (make-move-on-copy rather than make/unmake with undo)
// for simplicity; this can be optimized later once correctness is
// established via perft tests.
func (b *Board) Copy() *Board {
	nb := *b
	return &nb
}

// PieceAt returns the piece on a square (None if empty).
func (b *Board) PieceAt(sq Square) Piece {
	return b.Squares[sq]
}

// KingSquare finds the square of the given color's king.
// Returns NoSquare if not found (should not happen in a legal position).
func (b *Board) KingSquare(c Color) Square {
	target := MakePiece(King, c)
	for sq := Square(0); sq < 64; sq++ {
		if b.Squares[sq] == target {
			return sq
		}
	}
	return NoSquare
}

// String renders the board as an 8x8 text diagram, rank 8 at the top,
// for easy debugging in the terminal.
func (b *Board) String() string {
	s := ""
	for rank := 7; rank >= 0; rank-- {
		s += fmt.Sprintf("%d  ", rank+1)
		for file := 0; file < 8; file++ {
			s += b.Squares[MakeSquare(file, rank)].String() + " "
		}
		s += "\n"
	}
	s += "   a b c d e f g h\n"
	return s
}
