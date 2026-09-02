package eval

import "github.com/yourname/dyanis-chess-engine/internal/board"

const (
	doubledPawnPenalty  = 10
	isolatedPawnPenalty = 15
)

// passedPawnBonus is indexed by rank as advancement toward promotion:
// for a White pawn that's just its rank index (0 and 7 never actually
// hold a pawn — rank 0 is the back rank it started behind, rank 7 is
// where it would already have promoted). Values grow steeply near the
// end because a passed pawn on the 6th/7th rank is close to
// unstoppable, while one on the 3rd/4th is still a long way from
// costing the defender anything.
var passedPawnBonus = [8]int{0, 5, 10, 20, 35, 60, 100, 0}

// pawnMap[file][rank] records whether a pawn of one color sits there.
// Built once per pawnStructureScore/kingSafetyScore call and reused
// across every check (doubled, isolated, passed, shield) instead of
// rescanning b.Squares for each one separately.
type pawnMap [8][8]bool

func buildPawnMaps(b *board.Board) (white, black pawnMap) {
	for sqIdx, p := range b.Squares {
		if p.Type() != board.Pawn {
			continue
		}
		s := board.Square(sqIdx)
		if p.Color() == board.White {
			white[s.File()][s.Rank()] = true
		} else {
			black[s.File()][s.Rank()] = true
		}
	}
	return white, black
}

func fileCounts(pm pawnMap) [8]int {
	var counts [8]int
	for f := 0; f < 8; f++ {
		for r := 0; r < 8; r++ {
			if pm[f][r] {
				counts[f]++
			}
		}
	}
	return counts
}

// isIsolated reports whether the file has no friendly pawn on either
// neighboring file, given per-file pawn counts for one color.
func isIsolated(fileCount [8]int, file int) bool {
	left := file > 0 && fileCount[file-1] > 0
	right := file < 7 && fileCount[file+1] > 0
	return !left && !right
}

// isPassed reports whether a pawn of color at (file, rank) has no
// enemy pawn on its own file or either adjacent file that could still
// block or capture it on the way to promotion.
func isPassed(enemy pawnMap, file, rank int, color board.Color) bool {
	for nf := file - 1; nf <= file+1; nf++ {
		if nf < 0 || nf > 7 {
			continue
		}
		for nr := 0; nr < 8; nr++ {
			if !enemy[nf][nr] {
				continue
			}
			if color == board.White && nr > rank {
				return false
			}
			if color == board.Black && nr < rank {
				return false
			}
		}
	}
	return true
}

func passedBonus(rank int, color board.Color) int {
	idx := rank
	if color == board.Black {
		idx = 7 - rank
	}
	return passedPawnBonus[idx]
}

// pawnStructureScore returns White-minus-Black centipawns from
// doubled pawns, isolated pawns, and passed pawns.
func pawnStructureScore(b *board.Board) int {
	white, black := buildPawnMaps(b)
	whiteFiles := fileCounts(white)
	blackFiles := fileCounts(black)

	score := 0
	for f := 0; f < 8; f++ {
		if whiteFiles[f] > 1 {
			score -= doubledPawnPenalty * (whiteFiles[f] - 1)
		}
		if blackFiles[f] > 1 {
			score += doubledPawnPenalty * (blackFiles[f] - 1)
		}
		if whiteFiles[f] > 0 && isIsolated(whiteFiles, f) {
			score -= isolatedPawnPenalty * whiteFiles[f]
		}
		if blackFiles[f] > 0 && isIsolated(blackFiles, f) {
			score += isolatedPawnPenalty * blackFiles[f]
		}
	}

	for f := 0; f < 8; f++ {
		for r := 0; r < 8; r++ {
			if white[f][r] && isPassed(black, f, r, board.White) {
				score += passedBonus(r, board.White)
			}
			if black[f][r] && isPassed(white, f, r, board.Black) {
				score -= passedBonus(r, board.Black)
			}
		}
	}

	return score
}
