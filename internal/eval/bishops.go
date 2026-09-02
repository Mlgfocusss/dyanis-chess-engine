package eval

import "github.com/yourname/dyanis-chess-engine/internal/board"

// bishopPairBonus rewards having both bishops: together they cover
// both square colors, which no single bishop can. It's a flat bonus
// tied to the piece set as a whole rather than to any one square, so
// it doesn't fit naturally into the piece-square tables.
const bishopPairBonus = 30

func bishopPairScore(b *board.Board) int {
	white, black := 0, 0
	for _, p := range b.Squares {
		if p.Type() != board.Bishop {
			continue
		}
		if p.Color() == board.White {
			white++
		} else {
			black++
		}
	}

	score := 0
	if white >= 2 {
		score += bishopPairBonus
	}
	if black >= 2 {
		score -= bishopPairBonus
	}
	return score
}
