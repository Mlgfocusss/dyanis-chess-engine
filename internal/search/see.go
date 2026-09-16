package search

import "github.com/yourname/dyanis-chess-engine/internal/board"

// seeKnightOffsets, seeKingOffsets, seeBishopDirs, seeRookDirs, and
// seeOnBoard are local copies of the same geometry movegen.go already
// has (see movegen's knightOffsets/kingOffsets/bishopDirs/rookDirs) —
// duplicated rather than imported because they're unexported there,
// and because SEE needs something movegen.IsSquareAttacked doesn't
// provide: not just "is this square attacked" but WHICH square the
// least valuable attacker sits on, checked against a Squares array
// that gets progressively mutated mid-function as the simulated
// capture sequence removes pieces from it. Six lines of duplicated
// geometry is a smaller cost than reshaping movegen's API for one
// caller.
var seeKnightOffsets = [8][2]int{
	{1, 2}, {2, 1}, {-1, 2}, {-2, 1},
	{1, -2}, {2, -1}, {-1, -2}, {-2, -1},
}

var seeKingOffsets = [8][2]int{
	{1, 0}, {-1, 0}, {0, 1}, {0, -1},
	{1, 1}, {1, -1}, {-1, 1}, {-1, -1},
}

var seeBishopDirs = [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
var seeRookDirs = [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

func seeOnBoard(file, rank int) bool {
	return file >= 0 && file < 8 && rank >= 0 && rank < 8
}

// seeValue is SEE's own piece-value table — identical in spirit to
// pieceOrderingValue (see the move-ordering section of search.go) but
// kept as its own function rather than a shared reference, so SEE's
// values can be tuned independently later without the two accidentally
// drifting apart from a change meant for only one purpose.
func seeValue(t board.PieceType) int {
	switch t {
	case board.Pawn:
		return 100
	case board.Knight:
		return 320
	case board.Bishop:
		return 330
	case board.Rook:
		return 500
	case board.Queen:
		return 900
	case board.King:
		return 20000
	default:
		return 0
	}
}

// leastValuableAttacker scans squares for the cheapest piece belonging
// to `by` that attacks `to`, checking piece types in ascending value
// order (pawn, knight, bishop, rook, queen, king) and returning as
// soon as one is found — since types are checked cheapest-first, the
// first hit found IS the least valuable attacker; there's no need to
// compare candidates from different tiers against each other.
//
// Unlike movegen.IsSquareAttacked, this takes a raw [64]board.Piece
// array rather than a *board.Board: SEE calls this repeatedly against
// a working copy of the board's squares that it progressively empties
// out as the simulated exchange proceeds (see SEE below), and
// *board.Board has no clean way to represent "this square is
// temporarily vacated for the sake of a hypothetical" without an
// unwanted full Board.Copy() plus field surgery on every single step
// of the exchange.
func leastValuableAttacker(squares *[64]board.Piece, to board.Square, by board.Color) (sq board.Square, pt board.PieceType, ok bool) {
	f, r := to.File(), to.Rank()

	// Pawns: a pawn attacks diagonally FORWARD from its own side's
	// perspective, so to find one attacking `to`, look one rank BEHIND
	// `to` from the attacker's perspective — same reasoning as
	// movegen.IsSquareAttacked's pawnDir.
	pawnDir := -1
	if by == board.Black {
		pawnDir = 1
	}
	for _, df := range []int{-1, 1} {
		nf, nr := f+df, r+pawnDir
		if seeOnBoard(nf, nr) {
			candidate := board.MakeSquare(nf, nr)
			if squares[candidate] == board.MakePiece(board.Pawn, by) {
				return candidate, board.Pawn, true
			}
		}
	}

	for _, o := range seeKnightOffsets {
		nf, nr := f+o[0], r+o[1]
		if seeOnBoard(nf, nr) {
			candidate := board.MakeSquare(nf, nr)
			if squares[candidate] == board.MakePiece(board.Knight, by) {
				return candidate, board.Knight, true
			}
		}
	}

	if candidate, found := seeSlidingAttacker(squares, f, r, seeBishopDirs[:], by, board.Bishop); found {
		return candidate, board.Bishop, true
	}
	if candidate, found := seeSlidingAttacker(squares, f, r, seeRookDirs[:], by, board.Rook); found {
		return candidate, board.Rook, true
	}
	// Queen outranks both bishop and rook, so it's only checked once
	// neither of those has already matched — a queen slides along
	// both sets of directions, so both need checking here.
	if candidate, found := seeSlidingAttacker(squares, f, r, seeBishopDirs[:], by, board.Queen); found {
		return candidate, board.Queen, true
	}
	if candidate, found := seeSlidingAttacker(squares, f, r, seeRookDirs[:], by, board.Queen); found {
		return candidate, board.Queen, true
	}

	for _, o := range seeKingOffsets {
		nf, nr := f+o[0], r+o[1]
		if seeOnBoard(nf, nr) {
			candidate := board.MakeSquare(nf, nr)
			if squares[candidate] == board.MakePiece(board.King, by) {
				return candidate, board.King, true
			}
		}
	}

	return board.NoSquare, board.NoType, false
}

// seeSlidingAttacker walks each direction from (f, r) until it hits a
// piece or the edge of the board, and reports the FIRST piece hit if
// it belongs to `by` and matches `want` — same "first blocker wins"
// logic as movegen's unexported slidingAttack, but returning the
// attacking square (needed so SEE can remove it from squares once
// it's "used" in the simulated exchange) rather than just a yes/no.
func seeSlidingAttacker(squares *[64]board.Piece, f, r int, dirs [][2]int, by board.Color, want board.PieceType) (board.Square, bool) {
	for _, d := range dirs {
		nf, nr := f+d[0], r+d[1]
		for seeOnBoard(nf, nr) {
			sq := board.MakeSquare(nf, nr)
			p := squares[sq]
			if !p.IsNone() {
				if p.Color() == by && p.Type() == want {
					return sq, true
				}
				break // blocked either way — this direction is exhausted
			}
			nf += d[0]
			nr += d[1]
		}
	}
	return board.NoSquare, false
}

// SEE (Static Exchange Evaluation) estimates the material outcome of
// the full capture sequence on m.To, assuming both sides keep
// recapturing with their cheapest available attacker for as long as
// one is available (SEE deliberately ignores pins and checks — see the
// note below). Returns the net material change in centipawns from the
// perspective of the side making m: positive means the sequence nets
// material for them, negative means it's a loss even after every
// recapture is accounted for — the classic case MVV-LVA alone can't
// see, e.g. a pawn takes a defended knight that promptly gets
// recaptured by a rook, netting a LOSS for the side that "won" the
// knight for a moment.
//
// Returns 0 for a non-capture move — there's no exchange to evaluate.
//
// Implementation: the "swap-off" algorithm (see chessprogrammingwiki's
// "Static Exchange Evaluation" article for the reference version this
// follows). A gain[] array tracks, ply by ply, what's captured at each
// step of the sequence; the final backward pass turns that into a
// single number by having each side pick whichever is better between
// continuing the exchange or stopping. Unlike the classic bitboard
// version, this doesn't need a separate "x-ray" trick to notice a rook
// revealing itself once the bishop in front of it is gone —
// leastValuableAttacker does a full rescan of the target square after
// every removal, so a newly-exposed attacker just gets found again
// naturally on the next iteration. That's less optimized than
// incremental bitboard updates, but simpler and correct, in keeping
// with this project's stated correctness-first priority (see
// board.go's package comment).
//
// Known simplifications, both standard for a mailbox/array SEE:
//   - Recaptures are never checked for legality in the "doesn't leave
//     your own king in check" sense — verifying that at every step of
//     every simulated exchange would be far too slow to be worth it.
//     In the rare position where a "recapture" is actually pinned and
//     illegal, SEE can misjudge a capture's true value slightly.
//   - Promotions are only honored for the INITIATING move (m.Promotion
//     is read below) — a pawn that reaches the back rank mid-exchange
//     as a RECAPTURE is still valued as a pawn for the rest of the
//     sequence. Modeling every possible promotion in an arbitrarily
//     long exchange chain adds real complexity for a case that's rare
//     in practice.
func SEE(b *board.Board, m board.Move) int {
	if !m.IsCapture() {
		return 0
	}

	squares := b.Squares // [64]Piece is an array, not a slice — this copies it
	to := m.To
	fromSq := m.From

	var capturedType board.PieceType
	if m.Flag == board.EnPassantCapture {
		// The captured pawn sits one rank behind `to` (from the
		// capturing pawn's perspective), not on `to` itself — `to` is
		// empty until the capturing pawn lands there.
		capSq := board.MakeSquare(to.File(), fromSq.Rank())
		capturedType = squares[capSq].Type()
		squares[capSq] = board.None
	} else {
		capturedType = squares[to].Type()
	}

	movingPiece := squares[fromSq]
	attackerColor := movingPiece.Color()
	attackerType := movingPiece.Type()
	if m.Flag == board.PromotionCapture {
		// The piece landing on `to` after THIS move is whatever it
		// promotes to, not the pawn that made the move — its value
		// going forward in the exchange (if it gets recaptured) should
		// reflect that.
		attackerType = m.Promotion
	}

	// Play the initiating capture on the working copy: the attacker
	// now sits on `to`, and its origin square is empty — which is
	// exactly what lets a piece standing behind it (e.g. a rook behind
	// the pawn that just captured) show up as an attacker on the next
	// scan, with no separate x-ray bookkeeping needed.
	squares[to] = board.MakePiece(attackerType, attackerColor)
	squares[fromSq] = board.None

	gain := []int{seeValue(capturedType)}
	side := attackerColor.Opposite()
	lastValue := seeValue(attackerType)

	for {
		sq, pt, ok := leastValuableAttacker(&squares, to, side)
		if !ok {
			break
		}
		gain = append(gain, lastValue-gain[len(gain)-1])

		squares[to] = board.MakePiece(pt, side)
		squares[sq] = board.None

		lastValue = seeValue(pt)
		side = side.Opposite()
	}

	// Backward minimax pass: at each ply, whoever's "turn" it
	// conceptually is picks the better of (a) not making that
	// recapture at all — netting nothing further beyond what's already
	// banked at the previous ply — or (b) making it, netting whatever
	// the next ply resolved to, negated (a gain for the recapturer is a
	// loss from the previous attacker's perspective). Equivalent to
	// gain[i] = -max(-gain[i], gain[i+1]) from the reference algorithm,
	// written here as the min it reduces to.
	for i := len(gain) - 2; i >= 0; i-- {
		if -gain[i+1] < gain[i] {
			gain[i] = -gain[i+1]
		}
	}

	return gain[0]
}
