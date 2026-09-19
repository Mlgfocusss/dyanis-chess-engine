package eval

import "github.com/yourname/dyanis-chess-engine/internal/board"

const (
	doubledPawnPenalty  = 10
	isolatedPawnPenalty = 15
)

// passedPawnBonus is indexed by rank as advancement toward promotion:
// for a White pawn that's just its rank index (0 and 7 never actually
// hold a pawn — rank 0 is the back rank it started behind, rank 7 is
// where it would already have promoted). Values grow steeply near the
// end because a passed pawn on the 6th/7th rank is close to
// unstoppable, while one on the 3rd/4th is still a long way from
// costing the defender anything.
var passedPawnBonus = [8]int{0, 5, 10, 20, 35, 60, 100, 0}

// fileMask[f] is every square on file f — the standard bitboard way
// to ask "how many pawns are on file f" (popcount of pawns&fileMask[f])
// or "is file f open" ((pawns&fileMask[f]) == 0) without a 64-square
// scan. Shared with king_safety.go's open/semi-open-file check, so
// it's computed here once rather than twice.
var fileMask [8]board.Bitboard

// passedMask[color][sq] is every square a pawn of the OPPOSITE color
// could occupy that would stop a color-pawn on sq from being passed:
// the same file and the two adjacent files, on every rank between sq
// and the promotion square (exclusive of sq's own rank). A pawn is
// passed exactly when the enemy pawn bitboard has no bit set inside
// its color's mask for that square — see isPassed.
var passedMask [2][64]board.Bitboard

func init() {
	for f := 0; f < 8; f++ {
		for r := 0; r < 8; r++ {
			fileMask[f].Set(board.MakeSquare(f, r))
		}
	}

	for sqIdx := 0; sqIdx < 64; sqIdx++ {
		sq := board.Square(sqIdx)
		f, r := sq.File(), sq.Rank()

		var white, black board.Bitboard
		for nf := f - 1; nf <= f+1; nf++ {
			if nf < 0 || nf > 7 {
				continue
			}
			for nr := r + 1; nr < 8; nr++ {
				white.Set(board.MakeSquare(nf, nr))
			}
			for nr := 0; nr < r; nr++ {
				black.Set(board.MakeSquare(nf, nr))
			}
		}
		passedMask[board.White][sqIdx] = white
		passedMask[board.Black][sqIdx] = black
	}
}

// fileCounts returns, for each file, how many of `pawns` sit on it —
// a straight popcount per file mask instead of the [8][8]bool scan
// this used to be (buildPawnMaps/pawnMap are gone: every caller below
// works from the two piece bitboards, board.Board.Pieces(Pawn, color),
// directly).
func fileCounts(pawns board.Bitboard) [8]int {
	var counts [8]int
	for f := 0; f < 8; f++ {
		counts[f] = (pawns & fileMask[f]).Count()
	}
	return counts
}

// isIsolated reports whether the file has no friendly pawn on either
// neighboring file, given per-file pawn counts for one color.
func isIsolated(fileCount [8]int, file int) bool {
	left := file > 0 && fileCount[file-1] > 0
	right := file < 7 && fileCount[file+1] > 0
	return !left && !right
}

// isPassed reports whether a color-pawn on sq has no enemy pawn
// anywhere in its passedMask — its own file or either adjacent file,
// on any rank between sq and promotion — that could still block or
// capture it on the way there.
func isPassed(enemyPawns board.Bitboard, sq board.Square, color board.Color) bool {
	return enemyPawns&passedMask[color][sq] == 0
}

func passedBonus(rank int, color board.Color) int {
	idx := rank
	if color == board.Black {
		idx = 7 - rank
	}
	return passedPawnBonus[idx]
}

// pawnStructureScore returns White-minus-Black centipawns from
// doubled pawns, isolated pawns, and passed pawns.
// pawnStructureScore checks cache first (when non-nil), computing
// and storing the result only on a miss — see PawnCache's own doc
// comment for why this is worth doing (pawn structure repeats across
// huge swaths of a search tree, far more often than the position as a
// whole does).
func pawnStructureScore(b *board.Board, cache *PawnCache) int {
	whitePawns := b.Pieces(board.Pawn, board.White)
	blackPawns := b.Pieces(board.Pawn, board.Black)

	var key uint64
	if cache != nil {
		key = pawnCacheKey(whitePawns, blackPawns)
		if e := cache.entries[key&pawnCacheMask]; e.valid && e.key == key {
			return e.score
		}
	}

	score := computePawnStructureScore(whitePawns, blackPawns)

	if cache != nil {
		cache.entries[key&pawnCacheMask] = pawnCacheEntry{key: key, valid: true, score: score}
	}
	return score
}

// computePawnStructureScore is pawnStructureScore's actual logic,
// unchanged from before caching existed — just taking the two pawn
// bitboards directly as parameters (all it ever needed from *board.Board
// in the first place) so pawnStructureScore's cache check above can
// sit in front of it without duplicating the board.Pieces calls.
func computePawnStructureScore(whitePawns, blackPawns board.Bitboard) int {
	whiteFiles := fileCounts(whitePawns)
	blackFiles := fileCounts(blackPawns)

	score := 0
	for f := 0; f < 8; f++ {
		if whiteFiles[f] > 1 {
			score -= doubledPawnPenalty * (whiteFiles[f] - 1)
		}
		if blackFiles[f] > 1 {
			score += doubledPawnPenalty * (blackFiles[f] - 1)
		}
		if whiteFiles[f] > 0 && isIsolated(whiteFiles, f) {
			score -= isolatedPawnPenalty * whiteFiles[f]
		}
		if blackFiles[f] > 0 && isIsolated(blackFiles, f) {
			score += isolatedPawnPenalty * blackFiles[f]
		}
	}

	wp := whitePawns
	for wp != 0 {
		var sq board.Square
		sq, wp = wp.PopLSB()
		if isPassed(blackPawns, sq, board.White) {
			score += passedBonus(sq.Rank(), board.White)
		}
	}
	bp := blackPawns
	for bp != 0 {
		var sq board.Square
		sq, bp = bp.PopLSB()
		if isPassed(whitePawns, sq, board.Black) {
			score -= passedBonus(sq.Rank(), board.Black)
		}
	}

	return score
}
