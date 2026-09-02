package movegen

import "github.com/yourname/dyanis-chess-engine/internal/board"

// Status describes the outcome of a position for the side to move.
type Status int

const (
	Ongoing Status = iota
	Checkmate
	Stalemate
	// Note: draw by 50-move rule, threefold repetition, and insufficient
	// material are separate checks (repetition needs game history, which
	// isn't tracked yet) and are not covered here.
)

// InCheck reports whether the side to move's king is currently attacked.
func InCheck(b *board.Board) bool {
	return IsSquareAttacked(b, b.KingSquare(b.SideToMove), b.SideToMove.Opposite())
}

// GameStatus determines whether the side to move is checkmated,
// stalemated, or the game is still ongoing. This is what "the engine
// understands checkmate" actually means in code: not a special rule
// bolted on, but the natural conclusion of "zero legal moves" combined
// with "is the king attacked right now".
func GameStatus(b *board.Board) Status {
	if len(GenerateLegalMoves(b)) > 0 {
		return Ongoing
	}
	if InCheck(b) {
		return Checkmate
	}
	return Stalemate
}
