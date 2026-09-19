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
	squares [64]Piece

	SideToMove Color

	Castling CastlingRights

	// EnPassant is the square a pawn can capture on-the-fly to, i.e. the
	// square "behind" a pawn that just made a two-square advance.
	// NoSquare if no en passant capture is currently available.
	EnPassant Square

	HalfmoveClock  int // for the 50-move rule
	FullmoveNumber int

	// hash is the Polyglot-compatible Zobrist hash of this exact
	// position (see zobrist.go's Hash method). Kept as a plain stored
	// field, maintained INCREMENTALLY by MakeMove/MakeNullMove (XORed
	// in/out per changed square, castling right, en passant file, and
	// side to move) rather than recomputed from scratch on every call
	// — see zobrist.go's package comment for why that recomputation
	// was fine for occasional book lookups but too slow once this
	// became the transposition table's lookup key, called on every
	// search node. Every code path that builds a Board from nothing
	// (NewInitialBoard, FromFEN) computes this once via
	// computeHashFromScratch; Copy() carries it along automatically
	// since it's a plain uint64 field, not a pointer or slice.
	hash uint64

	// kingSq caches each side's king square (kingSq[White], kingSq[Black])
	// so KingSquare doesn't have to linear-scan all 64 squares on every
	// call — profiling showed that scan was a real cost, since
	// KingSquare is called once per pseudo-legal candidate move inside
	// GenerateLegalMoves' king-safety filter, i.e. very often. Kept in
	// sync by MakeMove/UnmakeMove whenever the moved piece is a king
	// (see move.go); MakeNullMove/UnmakeNullMove never move a piece, so
	// they don't touch this. Copy() carries it along automatically,
	// same as hash.
	//
	// NOT trusted blindly, though: a Board built via a raw struct
	// literal (several tests do this — set up squares directly without
	// going through NewInitialBoard/FromFEN) leaves this at its zero
	// value, Square(0) = a1, which is not NoSquare and could easily be
	// wrong. KingSquare() verifies the cached square still actually
	// holds that color's king before trusting it, and falls back to
	// the old full-scan (self-healing the cache for next time) if not
	// — so this is a pure performance cache, never a correctness
	// requirement on how a Board gets built.
	kingSq [2]Square

	// bb is a full bitboard snapshot of this exact position — one
	// Bitboard per (piece type, color), plus per-color and combined
	// occupancy (see bitboard.go). Maintained INCREMENTALLY by
	// MakeMove/UnmakeMove wherever they mutate squares, the same
	// discipline hash already follows (see hash's own comment above):
	// every put/remove/move on bb sits right next to the matching
	// squares assignment in move.go, rather than being derived by a
	// full rebuildBitboards() walk on every call. MakeNullMove/
	// UnmakeNullMove don't touch it, same reasoning as kingSq — a null
	// move never relocates a piece.
	//
	// Every code path that builds a Board from nothing
	// (NewInitialBoard, FromFEN) seeds this once via rebuildBitboards,
	// the same way those two seed hash via computeHashFromScratch.
	// Copy() carries it along automatically, being a plain value type
	// with no pointers or slices.
	//
	// Not yet read by anything outside this package or by
	// IsSquareAttacked/movegen — those still work the way they always
	// have. This field exists purely so the incremental bookkeeping in
	// move.go can be trusted (via VerifyBitboards, run across perft
	// the same way VerifyHash is) before anything starts depending on
	// it for real. A Board built via a raw struct literal (as two
	// existing test helpers do, in zobrist_test.go and eval_test.go)
	// leaves this at its zero value — which is correct as long as
	// those helpers also leave squares all-empty, which they do; if a
	// future raw-literal helper ever populates squares by hand without
	// going through NewInitialBoard/FromFEN, VerifyBitboards on that
	// Board would (correctly) report a mismatch.
	bb bitboards

	// materialPST is the incrementally-maintained White-minus-Black
	// sum of PieceValue (material_hook.go) over every piece on the
	// board — everything except the king, which PieceValue always
	// returns 0 for (see its own doc comment for why: its positional
	// value depends on the board-wide `phase`, not just its own
	// square, so it can't be tracked incrementally per-square the way
	// everything else here can). Maintained the same way hash and bb
	// are: addPieceValue/removePieceValue sit right next to the
	// matching bb.put/remove/move call in move.go and bitboard.go's
	// SetSquare, and NewInitialBoard/FromFEN seed it once via
	// rebuildMaterialPST, the same role computeHashFromScratch/
	// rebuildBitboards play for those two fields.
	materialPST int
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
		b.squares[MakeSquare(file, 0)] = MakePiece(backRank[file], White)
		b.squares[MakeSquare(file, 1)] = MakePiece(Pawn, White)
		b.squares[MakeSquare(file, 6)] = MakePiece(Pawn, Black)
		b.squares[MakeSquare(file, 7)] = MakePiece(backRank[file], Black)
	}
	b.kingSq[White] = MakeSquare(4, 0) // e1
	b.kingSq[Black] = MakeSquare(4, 7) // e8
	b.hash = b.computeHashFromScratch()
	b.bb = b.rebuildBitboards()
	b.materialPST = b.rebuildMaterialPST()
	return b
}

// Copy returns a deep copy of the board. No longer used by move
// generation or search — those now mutate the board in place via
// MakeMove/UnmakeMove (see move.go) instead of allocating a fresh
// Board per move. Still useful wherever a throwaway branch is wanted
// without having to track an Undo to unwind it, e.g. search.go's
// principalVariation, which walks a line forward purely to read it and
// has no matching "unmake" step to run.
func (b *Board) Copy() *Board {
	nb := *b
	return &nb
}

// PieceAt returns the piece on a square (None if empty).
func (b *Board) PieceAt(sq Square) Piece {
	return b.squares[sq]
}

// SquaresArray returns a copy of the full 64-square mailbox, for code
// that needs to scan every square (eval's material/mobility/pawn-
// structure terms, search's SEE working copy) rather than look up one
// square at a time (PieceAt does that). A copy, not a reference to the
// real board — [64]Piece is an array, and arrays copy on assignment/
// return in Go, the same fact Board.Copy() and SEE's own working copy
// already rely on — so mutating the result never touches b itself.
// This is the supported replacement for ranging over b.squares
// directly from outside package board (see SetSquare's doc comment
// for why direct field access, in either direction, stopped being
// safe once Board started keeping a second representation (bb)
// alongside squares).
func (b *Board) SquaresArray() [64]Piece {
	return b.squares
}

// KingSquare finds the square of the given color's king. Returns
// NoSquare if that color genuinely has no king on the board.
//
// Fast path: trusts the kingSq cache (see Board's doc comment) if the
// square it points to still actually holds that color's king — O(1),
// no scan. Slow path, for a Board whose cache is stale or was never
// set (built via a raw struct literal rather than NewInitialBoard/
// FromFEN): falls back to the original full 64-square scan, and heals
// the cache with whatever it finds so subsequent calls on this same
// Board take the fast path.
func (b *Board) KingSquare(c Color) Square {
	want := MakePiece(King, c)
	if sq := b.kingSq[c]; sq != NoSquare && b.squares[sq] == want {
		return sq
	}
	for sq := Square(0); sq < 64; sq++ {
		if b.squares[sq] == want {
			b.kingSq[c] = sq
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
			s += b.squares[MakeSquare(file, rank)].String() + " "
		}
		s += "\n"
	}
	s += "   a b c d e f g h\n"
	return s
}
