package eval

import "github.com/yourname/dyanis-chess-engine/internal/board"

const (
	shieldPawnBonus         = 10
	openFileKingPenalty     = 20
	semiOpenFileKingPenalty = 10
)

// kingSafetyScore returns a White-minus-Black king safety adjustment,
// scaled by phase — full weight in the middlegame, fading to zero as
// material comes off the board — the same taper the king PST already
// uses: a king stuck on an open file is a real liability when enemy
// rooks and queens are still around to use it, and much less of one
// once they aren't. At phase 0 the term is skipped entirely rather
// than computed and multiplied by zero.
func kingSafetyScore(b *board.Board, phase int) int {
	if phase == 0 {
		return 0
	}

	white, black := buildPawnMaps(b)
	whiteFiles := fileCounts(white)
	blackFiles := fileCounts(black)

	raw := 0
	if wk := b.KingSquare(board.White); wk != board.NoSquare {
		raw += kingSafetyTerm(wk, board.White, white, whiteFiles, blackFiles)
	}
	if bk := b.KingSquare(board.Black); bk != board.NoSquare {
		raw -= kingSafetyTerm(bk, board.Black, black, blackFiles, whiteFiles)
	}

	return raw * phase / maxPhase
}

// kingSafetyTerm scores one king in isolation: a bonus for each
// friendly pawn still standing on the shield squares directly in
// front of it, and a penalty for each nearby file (its own plus the
// two beside it) that's open or semi-open — a lane an enemy rook or
// queen could use without a pawn ever getting in the way.
func kingSafetyTerm(kingSq board.Square, color board.Color, ownPawns pawnMap, ownFiles, enemyFiles [8]int) int {
	file, rank := kingSq.File(), kingSq.Rank()
	score := 0

	shieldRank := rank + 1
	if color == board.Black {
		shieldRank = rank - 1
	}
	if shieldRank >= 0 && shieldRank < 8 {
		for f := file - 1; f <= file+1; f++ {
			if f < 0 || f > 7 {
				continue
			}
			if ownPawns[f][shieldRank] {
				score += shieldPawnBonus
			}
		}
	}

	for f := file - 1; f <= file+1; f++ {
		if f < 0 || f > 7 {
			continue
		}
		ownHere := ownFiles[f] > 0
		enemyHere := enemyFiles[f] > 0
		switch {
		case !ownHere && !enemyHere:
			score -= openFileKingPenalty
		case !ownHere && enemyHere:
			score -= semiOpenFileKingPenalty
		}
	}

	return score
}
