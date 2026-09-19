// Extension point for material+PST scoring, kept incrementally by
// Board the same way hash (zobrist.go) and bb (bitboard.go) already
// are — but the actual VALUES belong to internal/eval, not here: this
// package knows how to move pieces, not what a knight on d5 is worth.
// eval.go's init() plugs its real values in via SetPieceValueHook;
// until that happens, PieceValue is nil and every Board's materialPST
// just stays 0 (see addPieceValue/removePieceValue's nil guard) —
// safe, not a crash, for any binary that constructs a Board without
// ever importing eval (e.g. internal/perft's own tests).
package board

// PieceValue, once set, gives a piece's material+positional
// contribution while standing on sq, from that piece's OWN color's
// perspective (board applies the +/- White-relative sign itself —
// see addPieceValue/removePieceValue below — the same split
// eval.materialAndPstScore's own +=/-= already uses).
//
// Deliberately expected to return 0 for King: the king's positional
// value blends between midgame and endgame tables based on `phase`, a
// board-wide quantity that changes whenever ANY non-pawn piece
// anywhere is captured or promoted — not just when the king itself
// moves — so a per-square incremental hook can never correctly track
// it. eval.Evaluate computes the king's own term itself, directly
// (two table lookups off board.Board.KingSquare, already O(1), no
// scan needed regardless), on top of whatever this tracks.
var PieceValue func(p Piece, sq Square) int

// SetPieceValueHook registers the function Board's incremental
// materialPST tracking calls. Meant to be set exactly once, from an
// init() in the package that owns the real values — see
// internal/eval/pst.go.
func SetPieceValueHook(f func(p Piece, sq Square) int) {
	PieceValue = f
}

// addPieceValue/removePieceValue update b.materialPST for one piece
// being placed on or removed from sq — called from every place
// move.go/bitboard.go already calls bb.put/remove/move, the same way
// those calls sit next to the matching hash XOR. A nil PieceValue
// (eval never imported/registered) makes both a no-op, leaving
// materialPST at its zero value.
func (b *Board) addPieceValue(p Piece, sq Square) {
	if PieceValue == nil {
		return
	}
	v := PieceValue(p, sq)
	if p.Color() == White {
		b.materialPST += v
	} else {
		b.materialPST -= v
	}
}

func (b *Board) removePieceValue(p Piece, sq Square) {
	if PieceValue == nil {
		return
	}
	v := PieceValue(p, sq)
	if p.Color() == White {
		b.materialPST -= v
	} else {
		b.materialPST += v
	}
}

// rebuildMaterialPST derives materialPST from scratch off b.squares —
// the materialPST-side counterpart to computeHashFromScratch/
// rebuildBitboards: what seeds NewInitialBoard/FromFEN, and what
// VerifyMaterialPST compares the incrementally-maintained value
// against.
func (b *Board) rebuildMaterialPST() int {
	if PieceValue == nil {
		return 0
	}
	total := 0
	for sq, p := range b.squares {
		if p.IsNone() {
			continue
		}
		v := PieceValue(p, Square(sq))
		if p.Color() == White {
			total += v
		} else {
			total -= v
		}
	}
	return total
}

// MaterialPST returns the incrementally-maintained White-minus-Black
// sum of PieceValue over every piece PieceValue doesn't return 0 for
// (in practice: everything except the king — see PieceValue's own
// doc comment). Callers that also want the king's own contribution
// (eval.materialAndPstScore does) add that separately, on top of
// this.
func (b *Board) MaterialPST() int {
	return b.materialPST
}

// VerifyMaterialPST reports whether b.materialPST, maintained
// incrementally by MakeMove/UnmakeMove/SetSquare, matches a full
// recomputation from b.squares — the materialPST-side counterpart to
// VerifyHash/VerifyBitboards, meant to be used the same way: run
// across a perft tree while the incremental maintenance is still
// earning trust. Always true (0 == 0) if PieceValue was never
// registered — nothing to disagree about in that case.
func (b *Board) VerifyMaterialPST() bool {
	return b.materialPST == b.rebuildMaterialPST()
}
