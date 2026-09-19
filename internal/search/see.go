package search

import "github.com/yourname/dyanis-chess-engine/internal/board"

// seeBoard is SEE's own small, mutable bitboard snapshot of a
// position — deliberately NOT board.Board itself. SEE simulates a
// hypothetical capture sequence that doesn't correspond to any real
// sequence of legal moves (whoever's "turn" it conceptually is just
// keeps recapturing with their cheapest attacker, with no legality
// check on any recapture — see SEE's own doc comment below for why),
// so driving it through board.Board's real MakeMove/UnmakeMove at
// every step would be both more work than necessary and semantically
// wrong. Seeded once from the real Board via Pieces/Occupied, then
// mutated locally (put/remove) as the exchange proceeds — the same
// shape board.bitboards uses internally (see board/bitboard.go), kept
// here as its own small type since SEE's simulated sequence has
// nothing to do with the real board's own bb.
type seeBoard struct {
	pieces [2][7]board.Bitboard // [color][PieceType]; index 0 (NoType) unused
	occ    board.Bitboard
}

func newSeeBoard(b *board.Board) seeBoard {
	var sb seeBoard
	for _, c := range [2]board.Color{board.White, board.Black} {
		for pt := board.Pawn; pt <= board.King; pt++ {
			sb.pieces[c][pt] = b.Pieces(pt, c)
		}
	}
	sb.occ = b.Occupied()
	return sb
}

func squareBit(sq board.Square) board.Bitboard {
	var bit board.Bitboard
	bit.Set(sq)
	return bit
}

// bishopRayDirs/rookRayDirs mirror see_slow.go's seeBishopDirs/
// seeRookDirs exactly — the fixed direction-check order the old
// [64]Piece/manual-scan implementation used. Needed here only to
// break ties (see pickAttacker below); own copies rather than
// reusing see_slow.go's so see.go doesn't depend on a file slated for
// deletion once TestSEEMatchesSlow has served its purpose.
var bishopRayDirs = [][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
var rookRayDirs = [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
var knightRayDirs = [][2]int{
	{1, 2}, {2, 1}, {-1, 2}, {-2, 1},
	{1, -2}, {2, -1}, {-1, -2}, {-2, -1},
}

func stepSign(x int) int {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	default:
		return 0
	}
}

// pickKnightAttacker is pickAttacker's knight-specific counterpart —
// deliberately NOT pickAttacker itself: a knight's offsets are fixed
// jumps like (1,2) or (2,-1), not unit steps along one of 8 rays of
// arbitrary length the way a bishop/rook/queen's are, so matching by
// SIGN alone (stepSign(1)=1, stepSign(2)=1 -> (1,1), which appears
// nowhere in knightRayDirs) would never match any real knight offset
// at all. This compares the EXACT (unreduced) file/rank difference
// against knightRayDirs instead.
func pickKnightAttacker(to board.Square, attackers board.Bitboard) board.Square {
	if attackers.Count() <= 1 {
		sq, _ := attackers.PopLSB()
		return sq
	}
	best := board.NoSquare
	bestIdx := len(knightRayDirs)
	remaining := attackers
	for remaining != 0 {
		var sq board.Square
		sq, remaining = remaining.PopLSB()
		df := sq.File() - to.File()
		dr := sq.Rank() - to.Rank()
		for i, d := range knightRayDirs {
			if d[0] == df && d[1] == dr && i < bestIdx {
				bestIdx = i
				best = sq
			}
		}
	}
	return best
}

// pickAttacker resolves `attackers` (already known, from the magic-
// bitboard AND above, to be every by-colored piece of one specific
// type currently attacking `to`) down to a single square. The common
// case — zero or one bit set — is a plain O(1) PopLSB, no extra work.
// Used for bishop/rook/queen (sliding pieces — see pickKnightAttacker
// above for why knight needs its own version instead).
//
// When 2+ bits are set, which one gets returned isn't just cosmetic:
// recapturing with one instead of another can reveal a completely
// different piece as the next attacker (removing one blocker opens a
// line the other blocker never touched), changing the rest of the
// simulated exchange — see TestSEEMatchesSlow's own comment for a
// worked example. dirs disambiguates by matching each candidate's
// direction from `to` against the fixed order bishopRayDirs/
// rookRayDirs check in, reproducing exactly which one the old
// [64]Piece/manual-scan implementation (see_slow.go) would have
// found. This works because a magic-bitboard sliding attack set can
// only ever contain the FIRST blocker in each of the (at most) 4
// directions — anything beyond a blocker is excluded by construction
// — so at most one candidate bit exists per direction, and matching
// bit-to-direction is enough to pick the same one a real per-
// direction ray walk would have stopped on first.
func pickAttacker(to board.Square, attackers board.Bitboard, dirs [][2]int) board.Square {
	if attackers.Count() <= 1 {
		sq, _ := attackers.PopLSB()
		return sq
	}
	best := board.NoSquare
	bestDirIdx := len(dirs)
	remaining := attackers
	for remaining != 0 {
		var sq board.Square
		sq, remaining = remaining.PopLSB()
		df := stepSign(sq.File() - to.File())
		dr := stepSign(sq.Rank() - to.Rank())
		for i, d := range dirs {
			if d[0] == df && d[1] == dr && i < bestDirIdx {
				bestDirIdx = i
				best = sq
			}
		}
	}
	return best
}

func (sb *seeBoard) remove(c board.Color, pt board.PieceType, sq board.Square) {
	bit := squareBit(sq)
	sb.pieces[c][pt] &^= bit
	sb.occ &^= bit
}

func (sb *seeBoard) put(c board.Color, pt board.PieceType, sq board.Square) {
	bit := squareBit(sq)
	sb.pieces[c][pt] |= bit
	sb.occ |= bit
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

// leastValuableAttacker finds the cheapest by-colored piece in sb
// attacking `to`, checking piece types in ascending value order
// (pawn, knight, bishop, rook, queen, king) via the same table/magic
// lookups movegen.AttackersTo uses (board.PawnAttacks/KnightAttacks/
// BishopAttacks/RookAttacks/KingAttacks), returning as soon as one is
// found — since types are checked cheapest-first, the first hit found
// IS the least valuable attacker; there's no need to compare
// candidates from different tiers against each other. When more than
// one of the same type attacks, which one PopLSB happens to return
// doesn't matter — they're equally cheap by definition, and SEE only
// ever needs ONE least-valuable attacker per step, not all of them.
// pickPawnAttacker is pickAttacker's pawn-specific counterpart: same
// "cheap when unambiguous, disambiguate by the old fixed check order
// when tied" shape, but a pawn's two possible attacking squares
// aren't expressed as a general direction list — see_slow.go checked
// df=-1 before df=1 (see its `for _, df := range []int{-1, 1}`), so
// that's the order matched here.
func pickPawnAttacker(to board.Square, attackers board.Bitboard) board.Square {
	if attackers.Count() <= 1 {
		sq, _ := attackers.PopLSB()
		return sq
	}
	best := board.NoSquare
	bestIdx := 2
	remaining := attackers
	for remaining != 0 {
		var sq board.Square
		sq, remaining = remaining.PopLSB()
		df := sq.File() - to.File()
		for i, want := range [2]int{-1, 1} {
			if df == want && i < bestIdx {
				bestIdx = i
				best = sq
			}
		}
	}
	return best
}

func leastValuableAttacker(sb *seeBoard, to board.Square, by board.Color) (sq board.Square, pt board.PieceType, ok bool) {
	if attackers := board.PawnAttacks(by.Opposite(), to) & sb.pieces[by][board.Pawn]; attackers != 0 {
		return pickPawnAttacker(to, attackers), board.Pawn, true
	}
	if attackers := board.KnightAttacks(to) & sb.pieces[by][board.Knight]; attackers != 0 {
		return pickKnightAttacker(to, attackers), board.Knight, true
	}
	if attackers := board.BishopAttacks(to, sb.occ) & sb.pieces[by][board.Bishop]; attackers != 0 {
		return pickAttacker(to, attackers, bishopRayDirs), board.Bishop, true
	}
	if attackers := board.RookAttacks(to, sb.occ) & sb.pieces[by][board.Rook]; attackers != 0 {
		return pickAttacker(to, attackers, rookRayDirs), board.Rook, true
	}
	// Queen outranks both bishop and rook, so it's only checked once
	// neither of those has already matched — a queen slides along
	// both sets of directions, so both need checking here. Checked as
	// two SEPARATE tests (diagonal first, then rank/file), not one
	// combined bitboard, and each resolved through pickAttacker: when
	// two-or-more queens of the same color tie (whether across
	// diagonal vs rank/file, or multiple on the SAME direction
	// category), which one leastValuableAttacker reports isn't just a
	// cosmetic choice — recapturing with one instead of another can
	// reveal a completely different piece as the next attacker,
	// changing the rest of the exchange. This reproduces exactly
	// which queen the original [64]Piece/manual-scan implementation
	// (see_slow.go) would have picked; a bare PopLSB (lowest square
	// index) can silently pick a different, equally-valued queen
	// instead, changing the result on positions exactly like this.
	if attackers := board.BishopAttacks(to, sb.occ) & sb.pieces[by][board.Queen]; attackers != 0 {
		return pickAttacker(to, attackers, bishopRayDirs), board.Queen, true
	}
	if attackers := board.RookAttacks(to, sb.occ) & sb.pieces[by][board.Queen]; attackers != 0 {
		return pickAttacker(to, attackers, rookRayDirs), board.Queen, true
	}
	if attackers := board.KingAttacks(to) & sb.pieces[by][board.King]; attackers != 0 {
		sq, _ = attackers.PopLSB()
		return sq, board.King, true
	}
	return board.NoSquare, board.NoType, false
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
// naturally on the next iteration.
//
// Known simplifications, both standard for a SEE implementation like
// this one:
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

	sb := newSeeBoard(b)
	to := m.To
	fromSq := m.From

	var capturedType board.PieceType
	var capturedColor board.Color
	if m.Flag == board.EnPassantCapture {
		// The captured pawn sits one rank behind `to` (from the
		// capturing pawn's perspective), not on `to` itself — `to` is
		// empty until the capturing pawn lands there.
		capSq := board.MakeSquare(to.File(), fromSq.Rank())
		captured := b.PieceAt(capSq)
		capturedType = captured.Type()
		capturedColor = captured.Color()
		sb.remove(capturedColor, capturedType, capSq)
	} else {
		captured := b.PieceAt(to)
		capturedType = captured.Type()
		capturedColor = captured.Color()
		sb.remove(capturedColor, capturedType, to)
	}

	movingPiece := b.PieceAt(fromSq)
	attackerColor := movingPiece.Color()
	attackerType := movingPiece.Type()
	if m.Flag == board.PromotionCapture {
		// The piece landing on `to` after THIS move is whatever it
		// promotes to, not the pawn that made the move — its value
		// going forward in the exchange (if it gets recaptured) should
		// reflect that.
		attackerType = m.Promotion
	}

	// Play the initiating capture on the working snapshot: the
	// attacker now sits on `to` (whatever was captured there is
	// already removed, above — including the en passant case, where
	// `to` itself was always empty), and its origin square is
	// empty — which is exactly what lets a piece standing behind it
	// (e.g. a rook behind the pawn that just captured) show up as an
	// attacker on the next scan, with no separate x-ray bookkeeping
	// needed.
	sb.remove(attackerColor, movingPiece.Type(), fromSq)
	sb.put(attackerColor, attackerType, to)
	onTo, onToColor := attackerType, attackerColor

	gain := []int{seeValue(capturedType)}
	side := attackerColor.Opposite()
	lastValue := seeValue(attackerType)

	for {
		sq, pt, ok := leastValuableAttacker(&sb, to, side)
		if !ok {
			break
		}
		gain = append(gain, lastValue-gain[len(gain)-1])

		// Whoever currently sits on `to` just got captured — remove
		// them before the new attacker takes their place, same as the
		// plain array-overwrite (squares[to] = ...) the earlier
		// [64]Piece version did implicitly on every step.
		sb.remove(onToColor, onTo, to)
		sb.remove(side, pt, sq)
		sb.put(side, pt, to)
		onTo, onToColor = pt, side

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
