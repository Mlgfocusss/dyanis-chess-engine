package eval

import "github.com/yourname/dyanis-chess-engine/internal/board"

// Mobility weight per available destination square, in centipawns.
// These are deliberately small compared to material — a rook can
// have a dozen safe squares and that's still less than a third of a
// pawn's value. Mobility is meant to nudge between otherwise-similar
// positions, not let a piece "earn back" material by being active.
//
// Knights and bishops get the largest per-square weight: a knight or
// bishop with few squares is often a genuinely bad piece (hemmed in,
// no outpost), whereas a rook or queen usually has some mobility
// almost everywhere just from how far they reach, so the same raw
// count says less about the piece's quality.
const (
	knightMobilityWeight = 4
	bishopMobilityWeight = 3
	rookMobilityWeight   = 2
	queenMobilityWeight  = 1
)

var knightOffsets = [8][2]int{
	{1, 2}, {2, 1}, {2, -1}, {1, -2},
	{-1, -2}, {-2, -1}, {-2, 1}, {-1, 2},
}

var bishopDirs = [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
var rookDirs = [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

func onBoard(file, rank int) bool {
	return file >= 0 && file < 8 && rank >= 0 && rank < 8
}

// pawnAttackSquares reports, for each color, which squares that
// color's pawns attack. Computed once per Evaluate call (like
// gamePhase) and shared by every piece's mobility count below instead
// of being re-derived per piece.
func pawnAttackSquares(b *board.Board) (white, black [64]bool) {
	for sqIdx, p := range b.Squares {
		if p.Type() != board.Pawn {
			continue
		}
		s := board.Square(sqIdx)
		file, rank := s.File(), s.Rank()
		dir := 1
		target := &white
		if p.Color() == board.Black {
			dir = -1
			target = &black
		}
		for _, df := range []int{-1, 1} {
			f, r := file+df, rank+dir
			if onBoard(f, r) {
				target[board.MakeSquare(f, r)] = true
			}
		}
	}
	return white, black
}

// knightMobility counts squares a knight on s could move or capture
// to, excluding squares occupied by its own side and squares an enemy
// pawn attacks — a knight that's only "mobile" into a square it gets
// immediately traded off of isn't really mobile.
func knightMobility(b *board.Board, s board.Square, color board.Color, enemyPawnAtk *[64]bool) int {
	count := 0
	file, rank := s.File(), s.Rank()
	for _, o := range knightOffsets {
		f, r := file+o[0], rank+o[1]
		if !onBoard(f, r) {
			continue
		}
		dest := board.MakeSquare(f, r)
		if enemyPawnAtk[dest] {
			continue
		}
		occ := b.Squares[dest]
		if occ.IsNone() || occ.Color() != color {
			count++
		}
	}
	return count
}

// slidingMobility counts reachable squares along the given ray
// directions (bishop or rook directions; queen calls both), stopping
// at the first blocker in each direction the way real sliding moves
// do. Squares an enemy pawn attacks aren't counted (same "safe
// mobility" idea as knightMobility), but an occupied square still
// blocks the ray whether or not it counts.
func slidingMobility(b *board.Board, s board.Square, color board.Color, dirs [4][2]int, enemyPawnAtk *[64]bool) int {
	count := 0
	file, rank := s.File(), s.Rank()
	for _, d := range dirs {
		f, r := file+d[0], rank+d[1]
		for onBoard(f, r) {
			dest := board.MakeSquare(f, r)
			occ := b.Squares[dest]
			if occ.IsNone() {
				if !enemyPawnAtk[dest] {
					count++
				}
				f += d[0]
				r += d[1]
				continue
			}
			if occ.Color() != color && !enemyPawnAtk[dest] {
				count++
			}
			break
		}
	}
	return count
}

// mobilityScore returns White-minus-Black mobility bonus in
// centipawns for knights, bishops, rooks, and queens. Pawns aren't
// scored here (their advance is covered by pawnStructureScore's
// passed-pawn bonus), and neither is the king (huddling near safety
// is often correct, so a raw square count would send the wrong
// signal — king safety gets its own term instead).
func mobilityScore(b *board.Board) int {
	whitePawnAtk, blackPawnAtk := pawnAttackSquares(b)

	score := 0
	for sqIdx, p := range b.Squares {
		if p.IsNone() {
			continue
		}
		s := board.Square(sqIdx)
		enemyPawnAtk := &blackPawnAtk
		if p.Color() == board.Black {
			enemyPawnAtk = &whitePawnAtk
		}

		var v int
		switch p.Type() {
		case board.Knight:
			v = knightMobilityWeight * knightMobility(b, s, p.Color(), enemyPawnAtk)
		case board.Bishop:
			v = bishopMobilityWeight * slidingMobility(b, s, p.Color(), bishopDirs, enemyPawnAtk)
		case board.Rook:
			v = rookMobilityWeight * slidingMobility(b, s, p.Color(), rookDirs, enemyPawnAtk)
		case board.Queen:
			diag := slidingMobility(b, s, p.Color(), bishopDirs, enemyPawnAtk)
			straight := slidingMobility(b, s, p.Color(), rookDirs, enemyPawnAtk)
			v = queenMobilityWeight * (diag + straight)
		default:
			continue
		}

		if p.Color() == board.White {
			score += v
		} else {
			score -= v
		}
	}
	return score
}
