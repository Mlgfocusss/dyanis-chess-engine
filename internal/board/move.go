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

	// Incremental hash update: a null move only ever changes two
	// things about the position — en passant (always cleared, since
	// the capture window is only open for the one immediately
	// following move, and skipping a turn means that move didn't
	// happen) and side to move (every move, real or null, flips it).
	if b.EnPassant != NoSquare && enPassantCaptureIsPossible(b) {
		nb.hash ^= polyglotRandom64[772+b.EnPassant.File()]
	}
	nb.hash ^= polyglotRandom64[780]

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

	// Side to move is set here rather than at the end (as the original
	// non-hashed version did) because the incremental en passant hash
	// update below — specifically the "is this NEW en passant square
	// actually capturable" check — needs enPassantCaptureIsPossible to
	// see whose turn it's about to BE (the opponent, who could capture
	// on their next move), not whose turn it just was. Nothing else in
	// this function reads nb.SideToMove before this point, so moving
	// the assignment earlier changes nothing else.
	nb.SideToMove = movingColor.Opposite()

	// Incremental hash update, old en passant contribution: remove
	// whatever b's en passant square was contributing (only ever
	// nonzero if it was geometrically capturable — see
	// enPassantCaptureIsPossible), BEFORE nb.EnPassant gets
	// overwritten below. Every move clears or replaces it one way or
	// another, so the old contribution never survives unchanged.
	if b.EnPassant != NoSquare && enPassantCaptureIsPossible(b) {
		nb.hash ^= polyglotRandom64[772+b.EnPassant.File()]
	}

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
		captured := nb.Squares[capturedSq]
		nb.hash ^= polyglotRandom64[64*pieceIndex(captured)+int(capturedSq)]
		nb.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.From)]
		nb.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.To)]
		nb.Squares[capturedSq] = None
		nb.Squares[m.To] = mover
		nb.Squares[m.From] = None

	case CastleKingside, CastleQueenside:
		nb.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.From)]
		nb.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.To)]
		nb.Squares[m.To] = mover
		nb.Squares[m.From] = None
		rFrom, rTo := rookSquaresForCastle(movingColor, m.Flag)
		rook := nb.Squares[rFrom]
		nb.hash ^= polyglotRandom64[64*pieceIndex(rook)+int(rFrom)]
		nb.hash ^= polyglotRandom64[64*pieceIndex(rook)+int(rTo)]
		nb.Squares[rTo] = rook
		nb.Squares[rFrom] = None

	case Promotion, PromotionCapture:
		if m.Flag == PromotionCapture {
			captured := nb.Squares[m.To]
			nb.hash ^= polyglotRandom64[64*pieceIndex(captured)+int(m.To)]
		}
		promoted := MakePiece(m.Promotion, movingColor)
		nb.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.From)]
		nb.hash ^= polyglotRandom64[64*pieceIndex(promoted)+int(m.To)]
		nb.Squares[m.To] = promoted
		nb.Squares[m.From] = None

	case DoublePawnPush:
		nb.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.From)]
		nb.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.To)]
		nb.Squares[m.To] = mover
		nb.Squares[m.From] = None
		// The en passant target is the square the pawn "skipped over".
		skipped := (int(m.From) + int(m.To)) / 2
		nb.EnPassant = Square(skipped)

	default: // Quiet, Capture
		if m.Flag == Capture {
			captured := nb.Squares[m.To]
			nb.hash ^= polyglotRandom64[64*pieceIndex(captured)+int(m.To)]
		}
		nb.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.From)]
		nb.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.To)]
		nb.Squares[m.To] = mover
		nb.Squares[m.From] = None
	}

	// Update castling rights: moving a king or rook, or capturing a
	// rook on its home square, permanently removes the relevant right.
	oldCastling := nb.Castling
	nb.Castling &= castlingMaskAfterMoveFrom(m.From)
	nb.Castling &= castlingMaskAfterMoveFrom(m.To)

	// Incremental hash update, castling rights: Polyglot hashes each
	// of the four rights independently (XOR-toggle style — see
	// zobrist.go), so only bits that just got REVOKED need XORing out
	// here. Rights are only ever lost in this engine, never regained,
	// so "revoked" (was set, now isn't) is the only direction that can
	// happen — no XOR-in case to handle.
	revoked := oldCastling &^ nb.Castling
	if revoked&WhiteKingside != 0 {
		nb.hash ^= polyglotRandom64[768]
	}
	if revoked&WhiteQueenside != 0 {
		nb.hash ^= polyglotRandom64[769]
	}
	if revoked&BlackKingside != 0 {
		nb.hash ^= polyglotRandom64[770]
	}
	if revoked&BlackQueenside != 0 {
		nb.hash ^= polyglotRandom64[771]
	}

	// Incremental hash update, new en passant contribution: nb's own
	// SideToMove is already set to the opponent (see the top of this
	// function), which is exactly what enPassantCaptureIsPossible
	// needs — it asks "does the side about to move have a pawn
	// positioned to capture here", and after THIS move, that's
	// whoever's turn is next, not movingColor.
	if nb.EnPassant != NoSquare && enPassantCaptureIsPossible(nb) {
		nb.hash ^= polyglotRandom64[772+nb.EnPassant.File()]
	}

	// Side to move always toggles, every move.
	nb.hash ^= polyglotRandom64[780]

	if movingColor == Black {
		nb.FullmoveNumber = b.FullmoveNumber + 1
	}

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
