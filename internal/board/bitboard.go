// Bitboard scaffolding — step 1 of the array-to-bitboard migration
// described in TECHNICAL.md's planned rewrite. This file is
// deliberately inert: it adds a Bitboard type, precomputed non-sliding
// attack tables, and a way to derive a full bitboard snapshot from the
// existing squares array. Nothing in the package reads any of this yet
// — Board gains no new field here, and MakeMove/MakeNullMove/
// UnmakeMove are untouched. The point of this file existing on its own
// is to let the attack tables be tested for correctness (against
// IsSquareAttacked's current square-by-square logic) before anything
// starts depending on them, and before any incremental-maintenance
// bookkeeping gets added to move.go.
//
// Next step (not this file): give Board a `bb bitboards` field kept in
// sync incrementally by MakeMove/UnmakeMove/MakeNullMove — the same
// way b.hash is kept in sync today — verified against rebuildBitboards
// the same way VerifyHash checks b.hash, before anything consumes it.
package board

import "math/bits"

// Bitboard is a 64-bit set of squares. Bit i is set iff Square(i) is a
// member. Numbering matches Square itself (0 = a1 ... 63 = h8), so a
// Bitboard and the flat squares array agree on indexing without any
// translation.
type Bitboard uint64

// Test reports whether sq is a member of bb.
func (bb Bitboard) Test(sq Square) bool {
	return bb&(1<<uint(sq)) != 0
}

// Set adds sq to bb.
func (bb *Bitboard) Set(sq Square) {
	*bb |= 1 << uint(sq)
}

// Clear removes sq from bb.
func (bb *Bitboard) Clear(sq Square) {
	*bb &^= 1 << uint(sq)
}

// Count returns the number of set bits.
func (bb Bitboard) Count() int {
	return bits.OnesCount64(uint64(bb))
}

// PopLSB returns the lowest-indexed square in bb and bb with that bit
// cleared, for the standard iteration idiom:
//
//	for bb != 0 {
//	    var sq Square
//	    sq, bb = bb.PopLSB()
//	    // ... use sq ...
//	}
func (bb Bitboard) PopLSB() (Square, Bitboard) {
	sq := Square(bits.TrailingZeros64(uint64(bb)))
	return sq, bb & (bb - 1)
}

// bitboards is a full bitboard snapshot of a position: one board per
// (piece type, color) plus per-color and combined occupancy. Indexed
// the same way pieceIndex (zobrist.go) indexes polyglotRandom64's
// piece-on-square section — (pieceType-1)*2+pivot, pivot=1 for White —
// so the two schemes are trivially cross-checkable by eye, even though
// nothing here depends on zobrist.go.
type bitboards struct {
	pieces   [12]Bitboard
	occupied [2]Bitboard // occupied[White], occupied[Black]
	all      Bitboard
}

func (bb *bitboards) put(p Piece, sq Square) {
	bb.pieces[pieceIndex(p)].Set(sq)
	bb.occupied[p.Color()].Set(sq)
	bb.all.Set(sq)
}

// remove is put's inverse: take p off sq in every bitboard that
// tracks it (its own piece board, its color's occupancy, combined
// occupancy).
func (bb *bitboards) remove(p Piece, sq Square) {
	bb.pieces[pieceIndex(p)].Clear(sq)
	bb.occupied[p.Color()].Clear(sq)
	bb.all.Clear(sq)
}

// move relocates p from one square to another — shorthand for the
// remove+put pair that every non-capturing, non-promoting relocation
// in MakeMove/UnmakeMove needs.
func (bb *bitboards) move(p Piece, from, to Square) {
	bb.remove(p, from)
	bb.put(p, to)
}

// rebuildBitboards derives a full bitboard snapshot from b.squares —
// the bitboard-side counterpart to computeHashFromScratch (zobrist.go):
// an O(64) walk that's fine as a one-time build or a correctness check,
// never meant to sit on a hot path. Once a `bb` field exists on Board
// (next migration step), this is what seeds it in NewInitialBoard/
// FromFEN and what a verifyBitboards() correctness check compares
// against — exactly the role computeHashFromScratch already plays for
// b.hash.
func (b *Board) rebuildBitboards() bitboards {
	var bb bitboards
	for sq, p := range b.squares {
		if !p.IsNone() {
			bb.put(p, Square(sq))
		}
	}
	return bb
}

// VerifyBitboards reports whether b.bb, maintained incrementally by
// MakeMove/UnmakeMove/MakeNullMove, matches a full recomputation from
// b.squares. Exactly the bitboard-side counterpart to zobrist.go's
// VerifyHash, meant to be used the same way: run across a perft tree
// while the incremental maintenance in move.go is still earning trust.
// A mismatch means a missed bb update in move.go, not anything wrong
// with the position itself. Not on any hot path.
func (b *Board) VerifyBitboards() bool {
	return b.bb == b.rebuildBitboards()
}

// Pieces returns the bitboard of every square holding a piece of the
// given type and color. The zero value (no such piece anywhere) is a
// valid, empty Bitboard, same as any other query that comes up empty.
func (b *Board) Pieces(t PieceType, c Color) Bitboard {
	return b.bb.pieces[pieceIndex(MakePiece(t, c))]
}

// SetSquare places p on sq (or clears it, if p is None), keeping both
// the mailbox (squares) and the bitboard snapshot (bb) in sync. This
// is the supported way to hand-edit a Board outside the MakeMove/
// UnmakeMove discipline — test setup, puzzle/position construction,
// anything that isn't playing a real move.
//
// Writing directly to b.squares[sq] = p, which used to be safe back
// when squares was the only representation, now silently desyncs bb:
// Pieces/Occupied/OccupiedBy all read bb, not squares, so movegen
// would keep generating moves for a piece squares says was just
// removed (or miss one squares says was just placed) — VerifyBitboards
// would catch the mismatch, but nothing on the normal query path does,
// which is exactly how this surfaced (see movegen_test.go's
// TestCastlingAvailable, index-out-of-range in MakeMove from a
// pseudo-legal move whose From square squares and bb disagreed about).
func (b *Board) SetSquare(sq Square, p Piece) {
	old := b.squares[sq]
	if old == p {
		return
	}
	if !old.IsNone() {
		b.bb.remove(old, sq)
		b.removePieceValue(old, sq)
	}
	if !p.IsNone() {
		b.bb.put(p, sq)
		b.addPieceValue(p, sq)
	}
	b.squares[sq] = p
}

// Occupied returns the bitboard of every occupied square, either
// color — the occupancy argument magic.go's RookAttacks/BishopAttacks/
// QueenAttacks take, since a sliding piece's real attack set depends
// on every piece in its way, not just enemy pieces or just its own
// piece type.
func (b *Board) Occupied() Bitboard {
	return b.bb.all
}

// OccupiedBy returns the bitboard of every square occupied by a piece
// of color c.
func (b *Board) OccupiedBy(c Color) Bitboard {
	return b.bb.occupied[c]
}

// KnightAttacks returns the full destination set for a knight
// standing on sq — a table lookup, computed once at package init (see
// the init function below).
func KnightAttacks(sq Square) Bitboard {
	return knightAttacks[sq]
}

// KingAttacks returns the full destination set for a king standing on
// sq — a table lookup, same as KnightAttacks.
func KingAttacks(sq Square) Bitboard {
	return kingAttacks[sq]
}

// PawnAttacks returns the squares a pawn of color c standing on sq
// would attack (i.e. its two forward-diagonal captures, whichever are
// on the board). To find what's attacking sq — the more common
// query — index by the OPPOSITE color: PawnAttacks(by.Opposite(), sq)
// gives the squares a by-colored pawn would have to stand on to hit
// sq, so intersecting that with b.Pieces(Pawn, by) answers "is sq
// attacked by a by-colored pawn". This mirrors how movegen.
// IsSquareAttacked uses it.
func PawnAttacks(c Color, sq Square) Bitboard {
	return pawnAttacks[c][sq]
}

// --- precomputed non-sliding attack tables ---
//
// knightAttacks[sq] / kingAttacks[sq] / pawnAttacks[color][sq] are the
// full destination sets for a knight, king, or pawn-capture standing
// on sq, computed once at package init. These are meant to replace the
// offset loops in movegen.IsSquareAttacked's non-sliding checks (a
// later step): a lookup instead of re-deriving the same eight offsets
// on every call. Sliding pieces (bishop/rook/queen) aren't handled
// here — those need blocker-aware attacks (magic bitboards), which is
// a separate, larger piece of the migration.

var knightAttacks [64]Bitboard
var kingAttacks [64]Bitboard
var pawnAttacks [2][64]Bitboard // pawnAttacks[color][sq]

// knightAttackOffsets / kingAttackOffsets duplicate movegen's
// knightOffsets/kingOffsets. They can't simply be imported: board is
// lower in the dependency graph than movegen (movegen imports board,
// not the other way around), and duplicating eight small (file,rank)
// pairs is simpler than hoisting a shared package for this alone. If
// movegen's own offset tables ever change, these must change with
// them — once these tables start driving IsSquareAttacked (a later
// step), movegen_test.go's perft coverage would catch any drift
// between the two either way.
var knightAttackOffsets = [8][2]int{
	{1, 2}, {2, 1}, {-1, 2}, {-2, 1},
	{1, -2}, {2, -1}, {-1, -2}, {-2, -1},
}

var kingAttackOffsets = [8][2]int{
	{1, 0}, {-1, 0}, {0, 1}, {0, -1},
	{1, 1}, {1, -1}, {-1, 1}, {-1, -1},
}

func init() {
	for sqIdx := 0; sqIdx < 64; sqIdx++ {
		sq := Square(sqIdx)
		f, r := sq.File(), sq.Rank()

		for _, o := range knightAttackOffsets {
			if nf, nr := f+o[0], r+o[1]; onBoardFR(nf, nr) {
				knightAttacks[sqIdx].Set(MakeSquare(nf, nr))
			}
		}
		for _, o := range kingAttackOffsets {
			if nf, nr := f+o[0], r+o[1]; onBoardFR(nf, nr) {
				kingAttacks[sqIdx].Set(MakeSquare(nf, nr))
			}
		}
		for _, df := range [2]int{-1, 1} {
			nf := f + df
			if nf < 0 || nf > 7 {
				continue
			}
			if nr := r + 1; nr <= 7 {
				pawnAttacks[White][sqIdx].Set(MakeSquare(nf, nr))
			}
			if nr := r - 1; nr >= 0 {
				pawnAttacks[Black][sqIdx].Set(MakeSquare(nf, nr))
			}
		}
	}
}

func onBoardFR(file, rank int) bool {
	return file >= 0 && file < 8 && rank >= 0 && rank < 8
}
