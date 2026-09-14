package movegen

import "github.com/yourname/dyanis-chess-engine/internal/board"

// Status describes the outcome of a position for the side to move.
type Status int

const (
	Ongoing Status = iota
	Checkmate
	Stalemate

	// DrawFiftyMove: no capture or pawn move in the last 50 full moves
	// (100 half-moves).
	DrawFiftyMove

	// DrawRepetition: this exact position (same pieces, side to move,
	// castling rights, en passant square — i.e. same Zobrist hash) has
	// now occurred for the third time.
	DrawRepetition

	// DrawInsufficientMaterial: neither side has enough material left
	// to deliver checkmate by any sequence of legal moves — see
	// InsufficientMaterial's doc comment for exactly which material
	// configurations this covers (a deliberately simple, common
	// subset of the rule, not full dead-position detection).
	DrawInsufficientMaterial
)

// IsDraw reports whether s is a drawn result. Checkmate does NOT
// count — it's the one terminal Status that isn't a draw.
func (s Status) IsDraw() bool {
	return s == Stalemate || s == DrawFiftyMove || s == DrawRepetition || s == DrawInsufficientMaterial
}

// IsGameOver reports whether s represents a position where the game
// has already ended, by any means, as opposed to Ongoing.
func (s Status) IsGameOver() bool {
	return s != Ongoing
}

func (s Status) String() string {
	switch s {
	case Ongoing:
		return "ongoing"
	case Checkmate:
		return "checkmate"
	case Stalemate:
		return "stalemate"
	case DrawFiftyMove:
		return "draw by 50-move rule"
	case DrawRepetition:
		return "draw by threefold repetition"
	case DrawInsufficientMaterial:
		return "draw by insufficient material"
	default:
		return "unknown"
	}
}

// InCheck reports whether the side to move's king is currently attacked.
func InCheck(b *board.Board) bool {
	return IsSquareAttacked(b, b.KingSquare(b.SideToMove), b.SideToMove.Opposite())
}

// GameStatus determines whether the side to move is checkmated,
// stalemated, the 50-move rule has been reached, material is
// insufficient to force mate, or the game is still ongoing.
//
// This does NOT check threefold repetition — repetition is a property
// of the whole game's move history, which a single *board.Board
// doesn't carry (see the HalfmoveClock field's own comment for the
// same reasoning: it's intrinsic state a Board CAN carry, unlike a
// full position log). Use GameStatusWithHistory for that.
//
// search's negamax and quiescence call this form deliberately: they
// walk one board at a time without threading a position history
// through the recursion (see search.go), so they catch 50-move draws
// but not repetition ones. That asymmetry is acceptable for search's
// purposes — a fixed-depth tree can't loop forever the way real,
// unbounded play can, so the practical failure mode README warned
// about (the engine shuffling forever in a real game) is already
// closed by GameStatusWithHistory at the game-loop level below; not
// seeing repetitions inside its own lookahead just means search is
// occasionally a little less precise about lines that transpose back
// into an earlier position, not unsafe.
func GameStatus(b *board.Board) Status {
	if len(GenerateLegalMoves(b)) > 0 {
		if InsufficientMaterial(b) {
			return DrawInsufficientMaterial
		}
		if b.HalfmoveClock >= 100 {
			return DrawFiftyMove
		}
		return Ongoing
	}
	if InCheck(b) {
		return Checkmate
	}
	return Stalemate
}

// InsufficientMaterial reports whether neither side has enough
// material left on the board to force checkmate against any defense,
// under the common, deliberately simple rule set most engines
// implement:
//
//   - King vs king
//   - King + one minor piece (bishop or knight) vs king, either side
//   - King + bishop vs king + bishop, ONLY when both bishops are on
//     the same square color (opposite-colored bishops CAN force mate
//     in some positions, so those are left as Ongoing)
//
// Any pawn, rook, or queen on the board — for either side — rules
// this out immediately, since each of those can always contribute to
// forcing mate given enough of the rest of the game left to play.
//
// This deliberately does NOT attempt full dead-position detection.
// Some other material combinations (e.g. king + two knights vs king)
// are drawn in practice far more often than not, and some fortress-
// type positions with pawns still on the board are dead despite
// having "enough" material on paper — recognizing those needs real
// positional reasoning, not a material count, and is a reasonable
// future extension rather than this function's job.
func InsufficientMaterial(b *board.Board) bool {
	var whiteMinors, blackMinors int
	whiteBishop, blackBishop := board.NoSquare, board.NoSquare

	for sq := board.Square(0); sq < 64; sq++ {
		p := b.PieceAt(sq)
		if p.IsNone() {
			continue
		}
		switch p.Type() {
		case board.Pawn, board.Rook, board.Queen:
			return false
		case board.Bishop:
			if p.Color() == board.White {
				whiteMinors++
				whiteBishop = sq
			} else {
				blackMinors++
				blackBishop = sq
			}
		case board.Knight:
			if p.Color() == board.White {
				whiteMinors++
			} else {
				blackMinors++
			}
			// King: always present on both sides, doesn't affect the count.
		}
	}

	switch {
	case whiteMinors == 0 && blackMinors == 0:
		return true // K vs K
	case whiteMinors+blackMinors == 1:
		return true // K+minor vs K, either side
	case whiteMinors == 1 && blackMinors == 1 &&
		whiteBishop != board.NoSquare && blackBishop != board.NoSquare:
		// K+B vs K+B: only a draw if both bishops are stuck on the
		// same square color — if they're on opposite colors, mate is
		// still (rarely, but genuinely) achievable.
		return squareColor(whiteBishop) == squareColor(blackBishop)
	default:
		return false
	}
}

// squareColor returns 0 or 1 depending on which color square sq is —
// only meaningful as a way to compare TWO squares for "same color",
// not as a literal color value.
func squareColor(sq board.Square) int {
	return (sq.File() + sq.Rank()) % 2
}

// GameStatusWithHistory is GameStatus plus a threefold-repetition
// check, for callers that actually play out a real game and can keep
// a running log of it: cmd/cli's -play, the wasm frontend, and UCI's
// game loop should all use this instead of plain GameStatus.
//
// history must be the Zobrist hashes (board.Board.Hash()) of every
// position reached so far in the game, in order, INCLUDING b's own
// hash — i.e. append the hash of each position to your running slice
// as soon as it becomes current (the starting position included), and
// pass that same slice back in here every time you check status. See
// cmd/cli's playInteractive for the reference usage.
//
// Checkmate, stalemate, and the 50-move rule are checked first and
// take priority: per the standard rules, if a move delivers checkmate
// on the exact move that would otherwise also complete a repetition
// or reach the 50-move mark, the checkmate stands.
func GameStatusWithHistory(b *board.Board, history []uint64) Status {
	if s := GameStatus(b); s != Ongoing {
		return s
	}
	target := b.Hash()
	occurrences := 0
	for _, h := range history {
		if h == target {
			occurrences++
			if occurrences >= 3 {
				return DrawRepetition
			}
		}
	}
	return Ongoing
}
