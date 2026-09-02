package eval

import "github.com/yourname/dyanis-chess-engine/internal/board"

// -----------------------------------------------------------------------
// Piece-square tables.
//
// Indexed exactly like board.Board.Squares: index = rank*8+file, 0 = a1,
// 63 = h8, i.e. rank 1 is the FIRST row below and rank 8 is the LAST.
// That's the opposite order from how these tables are usually printed
// in engine write-ups (rank 8 on top, rank 1 on the bottom) — the rows
// below are that classic layout flipped vertically to match our index
// convention. All tables are written from White's point of view.
//
// For Black, don't build a second set of tables: mirror the square
// with sq^56, which flips the rank (0<->7, 1<->6, ...) while leaving
// the file untouched, and reuse the same White-oriented table. That's
// exactly the "same square relative to your own side" a mirrored
// table would give you.
// -----------------------------------------------------------------------

var pawnPST = [64]int{
	0, 0, 0, 0, 0, 0, 0, 0, // rank 1
	5, 10, 10, -20, -20, 10, 10, 5, // rank 2
	5, -5, -10, 0, 0, -10, -5, 5, // rank 3
	0, 0, 0, 20, 20, 0, 0, 0, // rank 4
	5, 5, 10, 25, 25, 10, 5, 5, // rank 5
	10, 10, 20, 30, 30, 20, 10, 10, // rank 6
	50, 50, 50, 50, 50, 50, 50, 50, // rank 7
	0, 0, 0, 0, 0, 0, 0, 0, // rank 8
}

var knightPST = [64]int{
	-50, -40, -30, -30, -30, -30, -40, -50, // rank 1
	-40, -20, 0, 5, 5, 0, -20, -40, // rank 2
	-30, 5, 10, 15, 15, 10, 5, -30, // rank 3
	-30, 0, 15, 20, 20, 15, 0, -30, // rank 4
	-30, 5, 15, 20, 20, 15, 5, -30, // rank 5
	-30, 0, 10, 15, 15, 10, 0, -30, // rank 6
	-40, -20, 0, 0, 0, 0, -20, -40, // rank 7
	-50, -40, -30, -30, -30, -30, -40, -50, // rank 8
}

var bishopPST = [64]int{
	-20, -10, -10, -10, -10, -10, -10, -20, // rank 1
	-10, 5, 0, 0, 0, 0, 5, -10, // rank 2
	-10, 10, 10, 10, 10, 10, 10, -10, // rank 3
	-10, 0, 10, 10, 10, 10, 0, -10, // rank 4
	-10, 5, 5, 10, 10, 5, 5, -10, // rank 5
	-10, 0, 5, 10, 10, 5, 0, -10, // rank 6
	-10, 0, 0, 0, 0, 0, 0, -10, // rank 7
	-20, -10, -10, -10, -10, -10, -10, -20, // rank 8
}

var rookPST = [64]int{
	0, 0, 0, 5, 5, 0, 0, 0, // rank 1
	-5, 0, 0, 0, 0, 0, 0, -5, // rank 2
	-5, 0, 0, 0, 0, 0, 0, -5, // rank 3
	-5, 0, 0, 0, 0, 0, 0, -5, // rank 4
	-5, 0, 0, 0, 0, 0, 0, -5, // rank 5
	-5, 0, 0, 0, 0, 0, 0, -5, // rank 6
	5, 10, 10, 10, 10, 10, 10, 5, // rank 7
	0, 0, 0, 0, 0, 0, 0, 0, // rank 8
}

var queenPST = [64]int{
	-20, -10, -10, -5, -5, -10, -10, -20, // rank 1
	-10, 0, 5, 0, 0, 0, 0, -10, // rank 2
	-10, 5, 5, 5, 5, 5, 0, -10, // rank 3
	0, 0, 5, 5, 5, 5, 0, -5, // rank 4
	-5, 0, 5, 5, 5, 5, 0, -5, // rank 5
	-10, 0, 5, 5, 5, 5, 0, -10, // rank 6
	-10, 0, 0, 0, 0, 0, 0, -10, // rank 7
	-20, -10, -10, -5, -5, -10, -10, -20, // rank 8
}

var kingMidgamePST = [64]int{
	20, 30, 10, 0, 0, 10, 30, 20, // rank 1
	20, 20, 0, 0, 0, 0, 20, 20, // rank 2
	-10, -20, -20, -20, -20, -20, -20, -10, // rank 3
	-20, -30, -30, -40, -40, -30, -30, -20, // rank 4
	-30, -40, -40, -50, -50, -40, -40, -30, // rank 5
	-30, -40, -40, -50, -50, -40, -40, -30, // rank 6
	-30, -40, -40, -50, -50, -40, -40, -30, // rank 7
	-30, -40, -40, -50, -50, -40, -40, -30, // rank 8
}

var kingEndgamePST = [64]int{
	-50, -30, -30, -30, -30, -30, -30, -50, // rank 1
	-30, -30, 0, 0, 0, 0, -30, -30, // rank 2
	-30, -10, 20, 30, 30, 20, -10, -30, // rank 3
	-30, -10, 30, 40, 40, 30, -10, -30, // rank 4
	-30, -10, 30, 40, 40, 30, -10, -30, // rank 5
	-30, -10, 20, 30, 30, 20, -10, -30, // rank 6
	-30, -20, -10, 0, 0, -10, -20, -30, // rank 7
	-50, -40, -30, -20, -20, -30, -40, -50, // rank 8
}

// mirror flips a square's rank while keeping its file, turning a
// White-oriented PST lookup into the equivalent square for Black.
func mirror(sq board.Square) board.Square {
	return board.Square(int(sq) ^ 56)
}

// pstValue looks up a piece's positional bonus/penalty on sq. phase is
// the value returned by gamePhase, passed in so it's only computed
// once per Evaluate call rather than once per piece.
func pstValue(p board.Piece, sq board.Square, phase int) int {
	idx := sq
	if p.Color() == board.Black {
		idx = mirror(sq)
	}
	switch p.Type() {
	case board.Pawn:
		return pawnPST[idx]
	case board.Knight:
		return knightPST[idx]
	case board.Bishop:
		return bishopPST[idx]
	case board.Rook:
		return rookPST[idx]
	case board.Queen:
		return queenPST[idx]
	case board.King:
		mg := kingMidgamePST[idx]
		eg := kingEndgamePST[idx]
		// Linear taper: phase == maxPhase -> pure mg, phase == 0 -> pure eg.
		return (mg*phase + eg*(maxPhase-phase)) / maxPhase
	default:
		return 0
	}
}
