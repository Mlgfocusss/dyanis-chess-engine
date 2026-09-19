package eval

import "github.com/yourname/dyanis-chess-engine/internal/board"

// Mobility weight per available destination square, in centipawns.
// These are deliberately small compared to material — a rook can
// have a dozen safe squares and that's still less than a third of a
// pawn's value. Mobility is meant to nudge between otherwise-similar
// positions, not let a piece "earn back" material by being active.
//
// Knights and bishops get the largest per-square weight: a knight or
// bishop with few squares is often a genuinely bad piece (hemmed in,
// no outpost), whereas a rook or queen usually has some mobility
// almost everywhere just from how far they reach, so the same raw
// count says less about the piece's quality.
const (
	knightMobilityWeight = 4
	bishopMobilityWeight = 3
	rookMobilityWeight   = 2
	queenMobilityWeight  = 1
)

// pawnAttackSquares reports, for each color, the full bitboard of
// squares that color's pawns attack — every pawn's board.PawnAttacks
// ORed together. Computed once per Evaluate call (like gamePhase) and
// shared by every piece's mobility count below instead of being
// re-derived per piece.
func pawnAttackSquares(b *board.Board) (white, black board.Bitboard) {
	wp := b.Pieces(board.Pawn, board.White)
	for wp != 0 {
		var sq board.Square
		sq, wp = wp.PopLSB()
		white |= board.PawnAttacks(board.White, sq)
	}
	bp := b.Pieces(board.Pawn, board.Black)
	for bp != 0 {
		var sq board.Square
		sq, bp = bp.PopLSB()
		black |= board.PawnAttacks(board.Black, sq)
	}
	return white, black
}

// knightMobility counts squares a knight on s could move or capture
// to, excluding squares occupied by its own side and squares an enemy
// pawn attacks — a knight that's only "mobile" into a square it gets
// immediately traded off of isn't really mobile. board.KnightAttacks
// already gives the full destination set as one table lookup; this is
// just that set with the two exclusions masked off and popcounted.
func knightMobility(b *board.Board, s board.Square, color board.Color, enemyPawnAtk board.Bitboard) int {
	targets := board.KnightAttacks(s) &^ b.OccupiedBy(color) &^ enemyPawnAtk
	return targets.Count()
}

// slidingMobility counts reachable squares from a bishop/rook/queen's
// attack bitboard (already blocker-aware — see board.BishopAttacks/
// RookAttacks/QueenAttacks, which stop at the first occupied square
// same as a real move would), excluding the mover's own pieces and
// squares an enemy pawn attacks (same "safe mobility" idea as
// knightMobility). Unlike the old ray-walk version, there's no
// separate "count this square but keep going" step needed: an
// enemy-pawn-attacked square that's otherwise empty still isn't a
// blocker, so it's already included in attacks and simply gets masked
// out of the count here without truncating the ray — exactly the
// original behavior, just derived from one magic lookup instead of a
// hand-walked loop.
func slidingMobility(b *board.Board, attacks board.Bitboard, color board.Color, enemyPawnAtk board.Bitboard) int {
	targets := attacks &^ b.OccupiedBy(color) &^ enemyPawnAtk
	return targets.Count()
}

// mobilityScore returns White-minus-Black mobility bonus in
// centipawns for knights, bishops, rooks, and queens. Pawns aren't
// scored here (their advance is covered by pawnStructureScore's
// passed-pawn bonus), and neither is the king (huddling near safety
// is often correct, so a raw square count would send the wrong
// signal — king safety gets its own term instead).
func mobilityScore(b *board.Board) int {
	whitePawnAtk, blackPawnAtk := pawnAttackSquares(b)
	occ := b.Occupied()

	score := 0
	for _, color := range [2]board.Color{board.White, board.Black} {
		sign := 1
		enemyPawnAtk := blackPawnAtk
		if color == board.Black {
			sign = -1
			enemyPawnAtk = whitePawnAtk
		}

		knights := b.Pieces(board.Knight, color)
		for knights != 0 {
			var sq board.Square
			sq, knights = knights.PopLSB()
			score += sign * knightMobilityWeight * knightMobility(b, sq, color, enemyPawnAtk)
		}

		bishops := b.Pieces(board.Bishop, color)
		for bishops != 0 {
			var sq board.Square
			sq, bishops = bishops.PopLSB()
			score += sign * bishopMobilityWeight * slidingMobility(b, board.BishopAttacks(sq, occ), color, enemyPawnAtk)
		}

		rooks := b.Pieces(board.Rook, color)
		for rooks != 0 {
			var sq board.Square
			sq, rooks = rooks.PopLSB()
			score += sign * rookMobilityWeight * slidingMobility(b, board.RookAttacks(sq, occ), color, enemyPawnAtk)
		}

		queens := b.Pieces(board.Queen, color)
		for queens != 0 {
			var sq board.Square
			sq, queens = queens.PopLSB()
			score += sign * queenMobilityWeight * slidingMobility(b, board.QueenAttacks(sq, occ), color, enemyPawnAtk)
		}
	}
	return score
}
