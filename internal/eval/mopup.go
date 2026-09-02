package eval

import "github.com/yourname/dyanis-chess-engine/internal/board"

// Mop-up.
//
// With overwhelming material (e.g. two queens and a rook vs a lone
// king), material + PST + mobility + king safety + pawn structure all
// saturate: nearly every legal move keeps the same score, because
// none of those terms distinguish "a move that tightens the net"
// from "a move that just shuffles a piece back and forth". Search has
// nothing to grab onto, so it can oscillate (e.g. Rg2<->Rh2) forever
// instead of converging on mate.
//
// Two small terms fix that once the game is *already* clearly won:
//   - push the losing king toward the edge/corner (that's where a
//     lone king actually gets mated)
//   - pull the winning king closer to the losing king (it has to help
//     box it in — a queen or rook alone usually can't mate without it)
//
// Both are gated tightly by mopupEligible so this NEVER fires in a
// normal, roughly balanced position — "rush your king forward" would
// be actively bad advice there, not just a no-op.
const (
	mopupCornerWeight   = 10 // per point of "how close to the edge" the losing king is
	mopupKingDistWeight = 4  // per point of "how close" the two kings are to each other

	// mopupMaterialThreshold: the winning side needs at least a rook's
	// worth (500cp) of non-pawn, non-king material more than the
	// loser before mop-up switches on — comfortably past any normal
	// swing in a competitive middlegame.
	mopupMaterialThreshold = 500

	// mopupPhaseCeiling: on top of the material gate, mop-up only
	// applies once the game has thinned out to at most half the
	// starting non-pawn material (see gamePhase in eval.go). Being a
	// rook up in a still-crowded middlegame is a real advantage, but
	// it doesn't mean "walk your king across the board" is good
	// advice yet — that's still true endgame-conversion technique.
	mopupPhaseCeiling = 12
)

// nonKingMaterial sums plain piece values (pieceValue, from eval.go —
// not the PST-adjusted score) for one color, excluding the king,
// which pieceValue already treats as worth 0.
func nonKingMaterial(b *board.Board, color board.Color) int {
	total := 0
	for _, p := range b.Squares {
		if p.IsNone() || p.Color() != color {
			continue
		}
		total += pieceValue(p.Type())
	}
	return total
}

// abs is a tiny local helper; Go's math.Abs is float64-only and these
// are always small board-coordinate ints.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// edgeDistance is how many squares in from the nearest edge a square
// is: 0 right on the rim, up to 3 at the four true center squares.
func edgeDistance(sq board.Square) int {
	file, rank := int(sq.File()), int(sq.Rank())
	d := file
	if 7-file < d {
		d = 7 - file
	}
	if rank < d {
		d = rank
	}
	if 7-rank < d {
		d = 7 - rank
	}
	return d
}

// kingDistance is Chebyshev distance between two squares — the number
// of king moves to get from one to the other, same metric the king
// itself moves by.
func kingDistance(a, b board.Square) int {
	df := abs(int(a.File()) - int(b.File()))
	dr := abs(int(a.Rank()) - int(b.Rank()))
	if df > dr {
		return df
	}
	return dr
}

// mopupTerm scores the mop-up bonus for the side whose king is
// strongKing against the side being driven back, whose king is
// weakKing. Always >= 0; the caller adds it for the winning color and
// subtracts it for the losing one.
func mopupTerm(strongKing, weakKing board.Square) int {
	cornered := 3 - edgeDistance(weakKing) // 0 (center) .. 3 (edge/corner)
	closeness := 7 - kingDistance(strongKing, weakKing)
	return cornered*mopupCornerWeight + closeness*mopupKingDistWeight
}

// mopupEligible reports whether color's material lead over the
// opponent is big enough, and the game thinned out enough, for
// king-hunting terms to be good advice rather than noise (or
// actively bad advice, if applied in a normal middlegame).
func mopupEligible(phase, myMaterial, theirMaterial int) bool {
	return phase <= mopupPhaseCeiling && myMaterial-theirMaterial >= mopupMaterialThreshold
}

// mopupScore returns a White-minus-Black centipawn adjustment. It's
// deliberately small (max ~58cp either way) relative to MateScore in
// search.go (900,000) and relative to a real material advantage
// (>=500cp just to switch on) — it's a tiebreaker among otherwise-
// equal moves in a won endgame, not a term that could ever outweigh
// an actual checkmate. Checkmate itself is detected in negamax via
// movegen.GameStatus before Evaluate is even called (see search.go),
// so this term can never mask or override a mate that's actually on
// the board.
func mopupScore(b *board.Board, phase int) int {
	wk := b.KingSquare(board.White)
	bk := b.KingSquare(board.Black)
	if wk == board.NoSquare || bk == board.NoSquare {
		return 0
	}

	whiteMat := nonKingMaterial(b, board.White)
	blackMat := nonKingMaterial(b, board.Black)

	score := 0
	if mopupEligible(phase, whiteMat, blackMat) {
		score += mopupTerm(wk, bk)
	}
	if mopupEligible(phase, blackMat, whiteMat) {
		score -= mopupTerm(bk, wk)
	}
	return score
}
