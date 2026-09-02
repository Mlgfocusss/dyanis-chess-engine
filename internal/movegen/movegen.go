// Package movegen generates legal chess moves for a given position.
//
// Strategy: generate pseudo-legal moves (moves that follow each piece's
// movement rules but might leave your own king in check), then filter
// out any move that leaves the mover's king attacked. This is the
// simplest correct approach and is what step 2 (perft tests) verifies.
// It is not the fastest approach (faster engines generate only-legal
// moves directly, or use pin/check masks) but correctness first,
// speed later — matches the project's stated priorities.
package movegen

import "github.com/yourname/dyanis-chess-engine/internal/board"

var knightOffsets = [8][2]int{
	{1, 2}, {2, 1}, {-1, 2}, {-2, 1},
	{1, -2}, {2, -1}, {-1, -2}, {-2, -1},
}

var kingOffsets = [8][2]int{
	{1, 0}, {-1, 0}, {0, 1}, {0, -1},
	{1, 1}, {1, -1}, {-1, 1}, {-1, -1},
}

var bishopDirs = [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
var rookDirs = [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

func onBoard(file, rank int) bool {
	return file >= 0 && file < 8 && rank >= 0 && rank < 8
}

// GenerateLegalMoves returns every legal move for the side to move.
func GenerateLegalMoves(b *board.Board) []board.Move {
	pseudo := generatePseudoLegalMoves(b)
	legal := make([]board.Move, 0, len(pseudo))

	for _, m := range pseudo {
		next := b.MakeMove(m)
		if !IsSquareAttacked(next, next.KingSquare(b.SideToMove), next.SideToMove) {
			legal = append(legal, m)
		}
	}
	return legal
}

// generatePseudoLegalMoves generates all moves following piece movement
// rules, without checking whether the mover's own king ends up in check.
func generatePseudoLegalMoves(b *board.Board) []board.Move {
	var moves []board.Move
	us := b.SideToMove

	for sq := board.Square(0); sq < 64; sq++ {
		p := b.PieceAt(sq)
		if p.IsNone() || p.Color() != us {
			continue
		}
		switch p.Type() {
		case board.Pawn:
			moves = append(moves, generatePawnMoves(b, sq, us)...)
		case board.Knight:
			moves = append(moves, generateOffsetMoves(b, sq, us, knightOffsets[:])...)
		case board.King:
			moves = append(moves, generateOffsetMoves(b, sq, us, kingOffsets[:])...)
			moves = append(moves, generateCastlingMoves(b, us)...)
		case board.Bishop:
			moves = append(moves, generateSlidingMoves(b, sq, us, bishopDirs[:])...)
		case board.Rook:
			moves = append(moves, generateSlidingMoves(b, sq, us, rookDirs[:])...)
		case board.Queen:
			moves = append(moves, generateSlidingMoves(b, sq, us, bishopDirs[:])...)
			moves = append(moves, generateSlidingMoves(b, sq, us, rookDirs[:])...)
		}
	}
	return moves
}

// generateOffsetMoves handles pieces that move to a fixed set of
// relative squares (knight, king) rather than sliding until blocked.
func generateOffsetMoves(b *board.Board, from board.Square, us board.Color, offsets [][2]int) []board.Move {
	var moves []board.Move
	f, r := from.File(), from.Rank()

	for _, o := range offsets {
		nf, nr := f+o[0], r+o[1]
		if !onBoard(nf, nr) {
			continue
		}
		to := board.MakeSquare(nf, nr)
		target := b.PieceAt(to)

		if target.IsNone() {
			moves = append(moves, board.Move{From: from, To: to, Flag: board.Quiet})
		} else if target.Color() != us {
			moves = append(moves, board.Move{From: from, To: to, Flag: board.Capture})
		}
		// else: blocked by own piece, skip
	}
	return moves
}

// generateSlidingMoves handles bishop/rook/queen: step along each
// direction until hitting a piece or the board edge.
func generateSlidingMoves(b *board.Board, from board.Square, us board.Color, dirs [][2]int) []board.Move {
	var moves []board.Move
	f, r := from.File(), from.Rank()

	for _, d := range dirs {
		nf, nr := f+d[0], r+d[1]
		for onBoard(nf, nr) {
			to := board.MakeSquare(nf, nr)
			target := b.PieceAt(to)

			if target.IsNone() {
				moves = append(moves, board.Move{From: from, To: to, Flag: board.Quiet})
			} else {
				if target.Color() != us {
					moves = append(moves, board.Move{From: from, To: to, Flag: board.Capture})
				}
				break // blocked either way, can't continue past this piece
			}
			nf += d[0]
			nr += d[1]
		}
	}
	return moves
}

var promotionTypes = []board.PieceType{board.Queen, board.Rook, board.Bishop, board.Knight}

func generatePawnMoves(b *board.Board, from board.Square, us board.Color) []board.Move {
	var moves []board.Move
	f, r := from.File(), from.Rank()

	forward := 1
	startRank := 1
	promoRank := 7
	if us == board.Black {
		forward = -1
		startRank = 6
		promoRank = 0
	}

	addForwardOrPromo := func(to board.Square, flag board.MoveFlag) {
		if to.Rank() == promoRank {
			for _, pt := range promotionTypes {
				pf := board.Promotion
				if flag == board.Capture {
					pf = board.PromotionCapture
				}
				moves = append(moves, board.Move{From: from, To: to, Flag: pf, Promotion: pt})
			}
		} else {
			moves = append(moves, board.Move{From: from, To: to, Flag: flag})
		}
	}

	// Single push.
	oneAheadRank := r + forward
	if onBoard(f, oneAheadRank) {
		oneAhead := board.MakeSquare(f, oneAheadRank)
		if b.PieceAt(oneAhead).IsNone() {
			addForwardOrPromo(oneAhead, board.Quiet)

			// Double push from the starting rank, only if both squares
			// ahead are empty.
			if r == startRank {
				twoAheadRank := r + 2*forward
				twoAhead := board.MakeSquare(f, twoAheadRank)
				if b.PieceAt(twoAhead).IsNone() {
					moves = append(moves, board.Move{From: from, To: twoAhead, Flag: board.DoublePawnPush})
				}
			}
		}
	}

	// Captures (including en passant).
	for _, df := range []int{-1, 1} {
		nf := f + df
		if !onBoard(nf, oneAheadRank) {
			continue
		}
		to := board.MakeSquare(nf, oneAheadRank)
		target := b.PieceAt(to)

		if !target.IsNone() && target.Color() != us {
			addForwardOrPromo(to, board.Capture)
		} else if to == b.EnPassant {
			moves = append(moves, board.Move{From: from, To: to, Flag: board.EnPassantCapture})
		}
	}

	return moves
}

// generateCastlingMoves checks the standard castling preconditions:
// rights still held, squares between king and rook empty, and the
// king does not start, pass through, or land on an attacked square.
// (Rights are already revoked elsewhere once the king or rook has
// moved or been captured — see board.MakeMove.)
func generateCastlingMoves(b *board.Board, us board.Color) []board.Move {
	var moves []board.Move
	rank := 0
	if us == board.Black {
		rank = 7
	}
	kingSq := board.MakeSquare(4, rank)

	if b.PieceAt(kingSq) != board.MakePiece(board.King, us) {
		return moves // king not on its home square, can't castle
	}
	if IsSquareAttacked(b, kingSq, us.Opposite()) {
		return moves // can't castle out of check
	}

	kingsideRight, queensideRight := board.WhiteKingside, board.WhiteQueenside
	if us == board.Black {
		kingsideRight, queensideRight = board.BlackKingside, board.BlackQueenside
	}

	if b.Castling&kingsideRight != 0 {
		fSq := board.MakeSquare(5, rank)
		gSq := board.MakeSquare(6, rank)
		if b.PieceAt(fSq).IsNone() && b.PieceAt(gSq).IsNone() &&
			!IsSquareAttacked(b, fSq, us.Opposite()) && !IsSquareAttacked(b, gSq, us.Opposite()) {
			moves = append(moves, board.Move{From: kingSq, To: gSq, Flag: board.CastleKingside})
		}
	}
	if b.Castling&queensideRight != 0 {
		dSq := board.MakeSquare(3, rank)
		cSq := board.MakeSquare(2, rank)
		bSq := board.MakeSquare(1, rank)
		if b.PieceAt(dSq).IsNone() && b.PieceAt(cSq).IsNone() && b.PieceAt(bSq).IsNone() &&
			!IsSquareAttacked(b, dSq, us.Opposite()) && !IsSquareAttacked(b, cSq, us.Opposite()) {
			moves = append(moves, board.Move{From: kingSq, To: cSq, Flag: board.CastleQueenside})
		}
	}
	return moves
}

// IsSquareAttacked reports whether `sq` is attacked by any piece of
// color `by`. Used both for check detection (is my king attacked?)
// and for castling legality (are the king's transit squares safe?).
func IsSquareAttacked(b *board.Board, sq board.Square, by board.Color) bool {
	if sq == board.NoSquare {
		return false
	}
	f, r := sq.File(), sq.Rank()

	// Pawn attacks: a pawn on `sq` would be attacked by an enemy pawn
	// one rank "behind" it (from the attacker's perspective, one rank
	// forward), diagonally adjacent.
	pawnDir := -1
	if by == board.Black {
		pawnDir = 1
	}
	for _, df := range []int{-1, 1} {
		nf, nr := f+df, r+pawnDir
		if onBoard(nf, nr) && b.PieceAt(board.MakeSquare(nf, nr)) == board.MakePiece(board.Pawn, by) {
			return true
		}
	}

	for _, o := range knightOffsets {
		nf, nr := f+o[0], r+o[1]
		if onBoard(nf, nr) && b.PieceAt(board.MakeSquare(nf, nr)) == board.MakePiece(board.Knight, by) {
			return true
		}
	}

	for _, o := range kingOffsets {
		nf, nr := f+o[0], r+o[1]
		if onBoard(nf, nr) && b.PieceAt(board.MakeSquare(nf, nr)) == board.MakePiece(board.King, by) {
			return true
		}
	}

	if slidingAttack(b, f, r, bishopDirs[:], by, board.Bishop, board.Queen) {
		return true
	}
	if slidingAttack(b, f, r, rookDirs[:], by, board.Rook, board.Queen) {
		return true
	}

	return false
}

// slidingAttack walks each direction from (f, r) looking for the first
// piece encountered; it's an attacker if it belongs to `by` and is one
// of the two allowed types (e.g. Bishop or Queen along diagonals).
func slidingAttack(b *board.Board, f, r int, dirs [][2]int, by board.Color, types ...board.PieceType) bool {
	for _, d := range dirs {
		nf, nr := f+d[0], r+d[1]
		for onBoard(nf, nr) {
			p := b.PieceAt(board.MakeSquare(nf, nr))
			if !p.IsNone() {
				if p.Color() == by {
					for _, t := range types {
						if p.Type() == t {
							return true
						}
					}
				}
				break
			}
			nf += d[0]
			nr += d[1]
		}
	}
	return false
}
