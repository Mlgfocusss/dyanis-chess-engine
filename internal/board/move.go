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

// NullUndo is what MakeNullMove needs to save so UnmakeNullMove can put
// the board back exactly as it was — same idea as Undo below, just for
// the narrower set of fields a null move touches.
type NullUndo struct {
	EnPassantBefore      Square
	HalfmoveClockBefore  int
	FullmoveNumberBefore int
	HashBefore           uint64
}

// MakeNullMove flips the side to move and clears en passant, mutating
// the board in place — used by search's null-move pruning ("if I skip
// my turn entirely, is this position still good enough for me? if even
// doing NOTHING doesn't let the opponent punish me, a real move
// certainly won't, so this subtree can be pruned"). Not a legal chess
// move — passing turns isn't something a real player can do — purely a
// search heuristic. Pair with UnmakeNullMove to restore the board
// afterward; every recursive caller is expected to do so before
// returning, the same discipline as MakeMove/UnmakeMove below.
//
// EnPassant is cleared, same as a real move would leave it unless that
// move was itself a double pawn push: the capture window is only ever
// open for the one immediately following move, and skipping a turn
// means that move didn't happen.
func (b *Board) MakeNullMove() NullUndo {
	undo := NullUndo{
		EnPassantBefore:      b.EnPassant,
		HalfmoveClockBefore:  b.HalfmoveClock,
		FullmoveNumberBefore: b.FullmoveNumber,
		HashBefore:           b.hash,
	}

	// Incremental hash update: a null move only ever changes two
	// things about the position — en passant (always cleared, since
	// the capture window is only open for the one immediately
	// following move, and skipping a turn means that move didn't
	// happen) and side to move (every move, real or null, flips it).
	// Both checks below run against the board's PRE-move state
	// (b.EnPassant, b.SideToMove), same as the copying version used —
	// nothing has been mutated yet at this point.
	if b.EnPassant != NoSquare && enPassantCaptureIsPossible(b) {
		b.hash ^= polyglotRandom64[772+b.EnPassant.File()]
	}
	b.hash ^= polyglotRandom64[780]

	b.EnPassant = NoSquare
	b.HalfmoveClock++
	if b.SideToMove == Black {
		b.FullmoveNumber++
	}
	b.SideToMove = b.SideToMove.Opposite()
	return undo
}

// UnmakeNullMove is MakeNullMove's exact inverse: restore every field
// straight from the snapshot NullUndo took, rather than trying to
// reverse the hash XORs by hand — simpler, and correct regardless of
// how MakeNullMove's own hash bookkeeping is implemented internally.
func (b *Board) UnmakeNullMove(u NullUndo) {
	b.SideToMove = b.SideToMove.Opposite()
	b.EnPassant = u.EnPassantBefore
	b.HalfmoveClock = u.HalfmoveClockBefore
	b.FullmoveNumber = u.FullmoveNumberBefore
	b.hash = u.HashBefore
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

// Undo is what MakeMove needs to save before mutating the board so
// UnmakeMove can put it back exactly as it was: the piece that stood
// on m.From before the move (the pawn itself for a promotion, not the
// promoted piece — that's what needs to reappear on m.From when
// unmaking), whatever piece was captured and the square it was
// captured FROM (differs from m.To only for en passant), and a
// snapshot of every board-wide field the move could have changed.
//
// The snapshot fields (CastlingBefore..HashBefore) are restored
// VERBATIM by UnmakeMove rather than reconstructed by re-deriving
// them from m — e.g. hash is put back by direct assignment, not by
// replaying MakeMove's XORs in reverse. That's deliberately simpler
// and, unlike a hand-reversed XOR sequence, correct independently of
// how MakeMove's own hash bookkeeping happens to be organized.
type Undo struct {
	MovedPiece     Piece
	CapturedPiece  Piece
	CapturedSquare Square // NoSquare if m wasn't a capture

	CastlingBefore       CastlingRights
	EnPassantBefore      Square
	HalfmoveClockBefore  int
	FullmoveNumberBefore int
	HashBefore           uint64
	MaterialPSTBefore    int
}

// MakeMove applies m to the board IN PLACE and returns an Undo that
// UnmakeMove needs to reverse it. This replaces the old copy-per-move
// approach (see board.go's Copy comment) now that correctness is
// established via perft — mutating in place avoids allocating a new
// Board on every node of the search tree. Every caller MUST pair this
// with a matching UnmakeMove(m, undo) once it's done looking at the
// resulting position (typically: right after the recursive call(s)
// that examine it return), the same "make, look, unmake" discipline
// negamax and GenerateLegalMoves below follow.
func (b *Board) MakeMove(m Move) Undo {
	mover := b.squares[m.From]
	movingColor := mover.Color()

	undo := Undo{
		MovedPiece:           mover,
		CapturedSquare:       NoSquare,
		CastlingBefore:       b.Castling,
		EnPassantBefore:      b.EnPassant,
		HalfmoveClockBefore:  b.HalfmoveClock,
		FullmoveNumberBefore: b.FullmoveNumber,
		HashBefore:           b.hash,
		MaterialPSTBefore:    b.materialPST,
	}

	// Incremental hash update, old en passant contribution: remove
	// whatever b's en passant square was contributing (only ever
	// nonzero if it was geometrically capturable — see
	// enPassantCaptureIsPossible), BEFORE b.EnPassant gets overwritten
	// below. This must run before b.SideToMove changes too —
	// enPassantCaptureIsPossible needs to see the mover's own side
	// still current, exactly as it did when it ran against the
	// not-yet-mutated receiver in the old copying version. Every move
	// clears or replaces the en passant square one way or another, so
	// the old contribution never survives unchanged.
	if b.EnPassant != NoSquare && enPassantCaptureIsPossible(b) {
		b.hash ^= polyglotRandom64[772+b.EnPassant.File()]
	}

	// Default: en passant is only available for one half-move.
	b.EnPassant = NoSquare

	// 50-move rule bookkeeping: reset on pawn move or capture.
	if mover.Type() == Pawn || m.IsCapture() {
		b.HalfmoveClock = 0
	} else {
		b.HalfmoveClock++
	}

	switch m.Flag {
	case EnPassantCapture:
		// The captured pawn is NOT on the destination square: it sits
		// on the same rank as the moving pawn, same file as the target.
		capturedSq := MakeSquare(m.To.File(), m.From.Rank())
		captured := b.squares[capturedSq]
		undo.CapturedPiece = captured
		undo.CapturedSquare = capturedSq
		b.hash ^= polyglotRandom64[64*pieceIndex(captured)+int(capturedSq)]
		b.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.From)]
		b.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.To)]
		b.bb.remove(captured, capturedSq)
		b.bb.move(mover, m.From, m.To)
		b.removePieceValue(captured, capturedSq)
		b.removePieceValue(mover, m.From)
		b.addPieceValue(mover, m.To)
		b.squares[capturedSq] = None
		b.squares[m.To] = mover
		b.squares[m.From] = None

	case CastleKingside, CastleQueenside:
		b.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.From)]
		b.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.To)]
		b.bb.move(mover, m.From, m.To)
		b.removePieceValue(mover, m.From)
		b.addPieceValue(mover, m.To)
		b.squares[m.To] = mover
		b.squares[m.From] = None
		rFrom, rTo := rookSquaresForCastle(movingColor, m.Flag)
		rook := b.squares[rFrom]
		b.hash ^= polyglotRandom64[64*pieceIndex(rook)+int(rFrom)]
		b.hash ^= polyglotRandom64[64*pieceIndex(rook)+int(rTo)]
		b.bb.move(rook, rFrom, rTo)
		b.removePieceValue(rook, rFrom)
		b.addPieceValue(rook, rTo)
		b.squares[rTo] = rook
		b.squares[rFrom] = None

	case Promotion, PromotionCapture:
		if m.Flag == PromotionCapture {
			captured := b.squares[m.To]
			undo.CapturedPiece = captured
			undo.CapturedSquare = m.To
			b.hash ^= polyglotRandom64[64*pieceIndex(captured)+int(m.To)]
			b.bb.remove(captured, m.To)
			b.removePieceValue(captured, m.To)
		}
		promoted := MakePiece(m.Promotion, movingColor)
		b.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.From)]
		b.hash ^= polyglotRandom64[64*pieceIndex(promoted)+int(m.To)]
		b.bb.remove(mover, m.From)
		b.bb.put(promoted, m.To)
		b.removePieceValue(mover, m.From)
		b.addPieceValue(promoted, m.To)
		b.squares[m.To] = promoted
		b.squares[m.From] = None

	case DoublePawnPush:
		b.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.From)]
		b.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.To)]
		b.bb.move(mover, m.From, m.To)
		b.removePieceValue(mover, m.From)
		b.addPieceValue(mover, m.To)
		b.squares[m.To] = mover
		b.squares[m.From] = None
		// The en passant target is the square the pawn "skipped over".
		skipped := (int(m.From) + int(m.To)) / 2
		b.EnPassant = Square(skipped)

	default: // Quiet, Capture
		if m.Flag == Capture {
			captured := b.squares[m.To]
			undo.CapturedPiece = captured
			undo.CapturedSquare = m.To
			b.hash ^= polyglotRandom64[64*pieceIndex(captured)+int(m.To)]
			b.bb.remove(captured, m.To)
			b.removePieceValue(captured, m.To)
		}
		b.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.From)]
		b.hash ^= polyglotRandom64[64*pieceIndex(mover)+int(m.To)]
		b.bb.move(mover, m.From, m.To)
		b.removePieceValue(mover, m.From)
		b.addPieceValue(mover, m.To)
		b.squares[m.To] = mover
		b.squares[m.From] = None
	}

	// King-square cache: only Quiet/Capture/CastleKingside/
	// CastleQueenside can ever move a king (a pawn can't promote INTO
	// a king, and en passant only ever moves a pawn) — m.To is already
	// the real king destination in every one of those cases (for
	// castling specifically, the normalized g1/c1/g8/c8 square, not
	// Polyglot's "king takes rook" encoding — see rookSquaresForCastle
	// and DecodeMove's normalizeCastling in internal/book). Placed
	// after the switch so it applies uniformly to all of them with one
	// check instead of duplicating it in each branch.
	if mover.Type() == King {
		b.kingSq[movingColor] = m.To
	}

	// Update castling rights: moving a king or rook, or capturing a
	// rook on its home square, permanently removes the relevant right.
	oldCastling := b.Castling
	b.Castling &= castlingMaskAfterMoveFrom(m.From)
	b.Castling &= castlingMaskAfterMoveFrom(m.To)

	// Incremental hash update, castling rights: Polyglot hashes each
	// of the four rights independently (XOR-toggle style — see
	// zobrist.go), so only bits that just got REVOKED need XORing out
	// here. Rights are only ever lost in this engine, never regained,
	// so "revoked" (was set, now isn't) is the only direction that can
	// happen — no XOR-in case to handle.
	revoked := oldCastling &^ b.Castling
	if revoked&WhiteKingside != 0 {
		b.hash ^= polyglotRandom64[768]
	}
	if revoked&WhiteQueenside != 0 {
		b.hash ^= polyglotRandom64[769]
	}
	if revoked&BlackKingside != 0 {
		b.hash ^= polyglotRandom64[770]
	}
	if revoked&BlackQueenside != 0 {
		b.hash ^= polyglotRandom64[771]
	}

	// Side to move flips here, right before the two checks below that
	// need to see it already flipped — nothing between here and the
	// end of the function reads it any earlier than that.
	b.SideToMove = movingColor.Opposite()

	// Incremental hash update, new en passant contribution:
	// b.SideToMove is already the opponent (just flipped above), which
	// is exactly what enPassantCaptureIsPossible needs — it asks "does
	// the side about to move have a pawn positioned to capture here",
	// and after THIS move, that's whoever's turn is next, not
	// movingColor.
	if b.EnPassant != NoSquare && enPassantCaptureIsPossible(b) {
		b.hash ^= polyglotRandom64[772+b.EnPassant.File()]
	}

	// Side to move always toggles, every move.
	b.hash ^= polyglotRandom64[780]

	if movingColor == Black {
		b.FullmoveNumber = undo.FullmoveNumberBefore + 1
	}

	return undo
}

// UnmakeMove reverses the most recent MakeMove(m), given the Undo it
// returned. Callers must unmake moves in the exact reverse order they
// were made (a plain stack discipline — search's recursion already
// gives this for free, since a node only unmakes the move it just made
// once its own recursive calls, which make and unmake their own moves
// first, have returned).
func (b *Board) UnmakeMove(m Move, u Undo) {
	movingColor := u.MovedPiece.Color()

	switch m.Flag {
	case EnPassantCapture:
		b.bb.move(u.MovedPiece, m.To, m.From)
		b.bb.put(u.CapturedPiece, u.CapturedSquare)
		b.squares[m.From] = u.MovedPiece
		b.squares[m.To] = None
		b.squares[u.CapturedSquare] = u.CapturedPiece

	case CastleKingside, CastleQueenside:
		b.bb.move(u.MovedPiece, m.To, m.From)
		b.squares[m.From] = u.MovedPiece
		b.squares[m.To] = None
		rFrom, rTo := rookSquaresForCastle(movingColor, m.Flag)
		rook := b.squares[rTo]
		b.bb.move(rook, rTo, rFrom)
		b.squares[rFrom] = rook
		b.squares[rTo] = None

	case Promotion, PromotionCapture:
		// The pawn goes back on m.From — MovedPiece was captured
		// BEFORE promotion happened, so it's still the pawn, never the
		// promoted piece.
		promoted := b.squares[m.To] // the piece MakeMove put there
		b.bb.remove(promoted, m.To)
		b.bb.put(u.MovedPiece, m.From)
		b.squares[m.From] = u.MovedPiece
		if m.Flag == PromotionCapture {
			b.bb.put(u.CapturedPiece, m.To)
			b.squares[m.To] = u.CapturedPiece
		} else {
			b.squares[m.To] = None
		}

	default: // Quiet, Capture, DoublePawnPush
		b.bb.move(u.MovedPiece, m.To, m.From)
		b.squares[m.From] = u.MovedPiece
		if m.Flag == Capture {
			b.bb.put(u.CapturedPiece, m.To)
			b.squares[m.To] = u.CapturedPiece
		} else {
			b.squares[m.To] = None
		}
	}

	// King-square cache: mirror image of the update in MakeMove — see
	// its comment. u.MovedPiece is the piece as it stood on m.From
	// before the move (never the promoted piece even for a promotion,
	// which can't apply to a king anyway), so checking its type here
	// is exactly equivalent to checking mover's type was King in
	// MakeMove.
	if u.MovedPiece.Type() == King {
		b.kingSq[movingColor] = m.From
	}

	b.Castling = u.CastlingBefore
	b.EnPassant = u.EnPassantBefore
	b.HalfmoveClock = u.HalfmoveClockBefore
	b.FullmoveNumber = u.FullmoveNumberBefore
	b.hash = u.HashBefore
	b.materialPST = u.MaterialPSTBefore
	b.SideToMove = movingColor
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
