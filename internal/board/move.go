package board

import "fmt"

// MoveFlag marks special move types that need extra handling beyond
// "piece goes from A to B", since those cases move more than one piece
// (castling), remove a piece not on the destination square (en passant),
// or change what piece ends up on the board (promotion).
type MoveFlag uint8

const (
	Quiet MoveFlag = iota
	Capture
	DoublePawnPush
	EnPassantCapture
	CastleKingside
	CastleQueenside
	Promotion
	PromotionCapture
)

// Move is a fully-specified move. Promotion is only meaningful when
// Flag is Promotion or PromotionCapture.
type Move struct {
	From, To  Square
	Flag      MoveFlag
	Promotion PieceType
}

func (m Move) String() string {
	if m.Flag == Promotion || m.Flag == PromotionCapture {
		letters := map[PieceType]string{Queen: "q", Rook: "r", Bishop: "b", Knight: "n"}
		return fmt.Sprintf("%s%s%s", m.From, m.To, letters[m.Promotion])
	}
	return fmt.Sprintf("%s%s", m.From, m.To)
}

// IsCapture reports whether this move removes an enemy piece
// (including en passant, which captures a pawn not on the To square).
func (m Move) IsCapture() bool {
	return m.Flag == Capture || m.Flag == EnPassantCapture || m.Flag == PromotionCapture
}

// MakeNullMove returns a copy of the board with the side to move
// flipped and nothing else changed on the board itself — used by
// search's null-move pruning ("if I skip my turn entirely, is this
// position still good enough for me? if even doing NOTHING doesn't
// let the opponent punish me, a real move certainly won't, so this
// subtree can be pruned"). Not a legal chess move — passing turns
// isn't something a real player can do — purely a search heuristic.
//
// EnPassant is cleared, same as a real move would leave it unless
// that move was itself a double pawn push: the capture window is only
// ever open for the one immediately following move, and skipping a
// turn means that move didn't happen.
func (b *Board) MakeNullMove() *Board {
	nb := b.Copy()
	nb.EnPassant = NoSquare
	nb.HalfmoveClock++
	if b.SideToMove == Black {
		nb.FullmoveNumber = b.FullmoveNumber + 1
	}
	nb.SideToMove = b.SideToMove.Opposite()
	return nb
}

// rookSquaresForCastle returns the rook's from/to squares for a given
// castling move, since castling relocates the rook as well as the king.
func rookSquaresForCastle(c Color, side MoveFlag) (from, to Square) {
	rank := 0
	if c == Black {
		rank = 7
	}
	if side == CastleKingside {
		return MakeSquare(7, rank), MakeSquare(5, rank)
	}
	return MakeSquare(0, rank), MakeSquare(3, rank)
}

// MakeMove applies m to a *copy* of the board and returns the new board.
// The receiver is left untouched, which keeps move generation simple
// (generate pseudo-legal move -> try it on a copy -> check king safety)
// at the cost of allocating a new Board per move. This is the "easy but
// not fastest" approach the project plan accepts for the early steps;
// a make/unmake-with-undo version can replace it later once perft
// tests pass and profiling shows it matters.
func (b *Board) MakeMove(m Move) *Board {
	nb := b.Copy()

	mover := nb.Squares[m.From]
	movingColor := mover.Color()

	// Default: en passant is only available for one half-move.
	nb.EnPassant = NoSquare

	// 50-move rule bookkeeping: reset on pawn move or capture.
	if mover.Type() == Pawn || m.IsCapture() {
		nb.HalfmoveClock = 0
	} else {
		nb.HalfmoveClock++
	}

	switch m.Flag {
	case EnPassantCapture:
		// The captured pawn is NOT on the destination square: it sits
		// on the same rank as the moving pawn, same file as the target.
		capturedSq := MakeSquare(m.To.File(), m.From.Rank())
		nb.Squares[capturedSq] = None
		nb.Squares[m.To] = mover
		nb.Squares[m.From] = None

	case CastleKingside, CastleQueenside:
		nb.Squares[m.To] = mover
		nb.Squares[m.From] = None
		rFrom, rTo := rookSquaresForCastle(movingColor, m.Flag)
		nb.Squares[rTo] = nb.Squares[rFrom]
		nb.Squares[rFrom] = None

	case Promotion, PromotionCapture:
		nb.Squares[m.To] = MakePiece(m.Promotion, movingColor)
		nb.Squares[m.From] = None

	case DoublePawnPush:
		nb.Squares[m.To] = mover
		nb.Squares[m.From] = None
		// The en passant target is the square the pawn "skipped over".
		skipped := (int(m.From) + int(m.To)) / 2
		nb.EnPassant = Square(skipped)

	default: // Quiet, Capture
		nb.Squares[m.To] = mover
		nb.Squares[m.From] = None
	}

	// Update castling rights: moving a king or rook, or capturing a
	// rook on its home square, permanently removes the relevant right.
	nb.Castling &= castlingMaskAfterMoveFrom(m.From)
	nb.Castling &= castlingMaskAfterMoveFrom(m.To)

	if movingColor == Black {
		nb.FullmoveNumber = b.FullmoveNumber + 1
	}
	nb.SideToMove = movingColor.Opposite()

	return nb
}

// castlingMaskAfterMoveFrom returns a mask to AND into Castling rights
// when a piece moves to-or-from the given square: home squares of kings
// and rooks. Any other square returns a mask that changes nothing.
func castlingMaskAfterMoveFrom(sq Square) CastlingRights {
	switch sq {
	case MakeSquare(4, 0): // e1, White king home
		return ^(WhiteKingside | WhiteQueenside)
	case MakeSquare(0, 0): // a1, White queenside rook
		return ^WhiteQueenside
	case MakeSquare(7, 0): // h1, White kingside rook
		return ^WhiteKingside
	case MakeSquare(4, 7): // e8, Black king home
		return ^(BlackKingside | BlackQueenside)
	case MakeSquare(0, 7): // a8, Black queenside rook
		return ^BlackQueenside
	case MakeSquare(7, 7): // h8, Black kingside rook
		return ^BlackKingside
	default:
		return ^CastlingRights(0)
	}
}
