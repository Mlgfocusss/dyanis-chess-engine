// Package eval scores a position from the perspective of the side to
// move: positive means the side to move is better off.
//
// Step 5 of the plan: material (step 3) and piece-square tables (step
// 4), plus five more positional terms, each in its own file:
//   - mobility        (mobility.go)
//   - pawn structure  (pawns.go)
//   - king safety     (king_safety.go)
//   - the bishop pair (bishops.go)
//   - mop-up          (mopup.go) — king-hunting technique once a
//     material advantage is already decisive, so search has something
//     to converge on instead of shuffling a piece forever in a
//     trivially won position (e.g. K+2Q+R vs K)
//
// Every term below is computed as a single White-minus-Black number,
// not "side to move minus other side". Evaluate sums all the terms
// and flips the sign exactly once at the end if Black is to move,
// instead of every term re-deriving "am I SideToMove or not" for
// every piece — cheaper, and it means a term's own tests can just
// think in absolute White/Black terms.
package eval

import "github.com/yourname/dyanis-chess-engine/internal/board"

// Material values in centipawns, the conventional unit (1 pawn = 100).
//
// Knight and Bishop are set equal (300 each) rather than the common
// 320/330 split some engines use. That split is a statistical average
// over many games — bishops tend to edge out knights on average — but
// it's not true position by position: knights are stronger in closed
// positions and on outposts, bishops in open positions with long
// diagonals. Baking a fixed asymmetry into material score would be
// encoding an average as if it were always true. The real, position-
// dependent difference lives in the piece-square tables and in
// mobility instead of here.
const (
	PawnValue   = 100
	KnightValue = 300
	BishopValue = 300
	RookValue   = 500
	QueenValue  = 900
)

func pieceValue(t board.PieceType) int {
	switch t {
	case board.Pawn:
		return PawnValue
	case board.Knight:
		return KnightValue
	case board.Bishop:
		return BishopValue
	case board.Rook:
		return RookValue
	case board.Queen:
		return QueenValue
	default: // King: material-count doesn't need to value it, it's never captured
		return 0
	}
}

// materialAndPstScore returns material + piece-square value, White
// minus Black. Almost entirely a single field read: b.MaterialPST()
// is the incrementally-maintained sum over every piece except the
// king (see board/material_hook.go — MakeMove/UnmakeMove/SetSquare
// keep it in sync as pieces move, so nothing here has to rescan the
// board). Only the king's own term still gets computed fresh on every
// call, since its value blends between midgame/endgame PSTs based on
// `phase` — a board-wide quantity that changes on every non-pawn
// capture or promotion anywhere, not just when the king itself moves,
// so it can never be folded into that same incremental sum. Still
// O(1) either way: two table lookups off board.Board.KingSquare
// (itself already cached), not a scan.
func materialAndPstScore(b *board.Board, phase int) int {
	score := b.MaterialPST()

	if wk := b.KingSquare(board.White); wk != board.NoSquare {
		score += pstValue(board.MakePiece(board.King, board.White), wk, phase)
	}
	if bk := b.KingSquare(board.Black); bk != board.NoSquare {
		score -= pstValue(board.MakePiece(board.King, board.Black), bk, phase)
	}
	return score
}

// phaseWeight is how much each piece type counts toward "how much
// midgame material is still on the board". Values follow the common
// convention: N/B = 1, R = 2, Q = 4. A full starting set of minor/major
// pieces (4N+4B+4R+2Q) sums to maxPhase = 24; an endgame with just
// kings and pawns sums to 0.
func phaseWeight(t board.PieceType) int {
	switch t {
	case board.Knight, board.Bishop:
		return 1
	case board.Rook:
		return 2
	case board.Queen:
		return 4
	default:
		return 0
	}
}

const maxPhase = 24

// gamePhase counts how much non-pawn material remains on the board,
// clamped to [0, maxPhase]. maxPhase means "still midgame material
// out there", 0 means "no minor/major pieces left, deep endgame".
// One Count() popcount per (piece type, color) pair instead of a
// 64-square scan.
func gamePhase(b *board.Board) int {
	phase := 0
	for _, color := range [2]board.Color{board.White, board.Black} {
		for pt := board.Knight; pt <= board.Queen; pt++ {
			phase += phaseWeight(pt) * b.Pieces(pt, color).Count()
		}
	}
	if phase > maxPhase {
		phase = maxPhase
	}
	return phase
}

// Evaluate returns a centipawn score from the perspective of the side
// to move: positive means the side to move is ahead. cache, when
// non-nil, is used for pawn-structure scoring — see PawnCache's own
// doc comment for what it's for and why it's threaded in here rather
// than kept as a package-level global. Passing nil (as every existing
// eval package test does) just means pawn structure gets recomputed
// from scratch every call, same as before caching existed — never
// wrong, only slower.
//
// TODO(later steps): real king danger from actual enemy attackers
// (not just open/semi-open files), space, outposts.
func Evaluate(b *board.Board, cache *PawnCache) int {
	phase := gamePhase(b)

	score := materialAndPstScore(b, phase)
	score += mobilityScore(b)
	score += pawnStructureScore(b, cache)
	score += kingSafetyScore(b, phase)
	score += bishopPairScore(b)
	score += mopupScore(b, phase)

	if b.SideToMove == board.Black {
		score = -score
	}
	return score
}
