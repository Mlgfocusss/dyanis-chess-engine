package search

import (
	"testing"

	"github.com/yourname/dyanis-chess-engine/internal/board"
)

func BenchmarkBestMoveDepth6(b *testing.B) {
	for i := 0; i < b.N; i++ {
		bd := board.NewInitialBoard()
		BestMove(bd, 6)
	}
}
