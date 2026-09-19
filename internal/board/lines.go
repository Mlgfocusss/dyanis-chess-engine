// Line geometry between pairs of squares — precomputed once at
// package init, same pattern as magic.go's attack tables. This is
// pure geometry with no notion of what's actually on the board; it's
// the building block pin detection and check-evasion move generation
// (see TECHNICAL.md's planned pin-mask/checkers-mask legal-move
// generator) both need: "if a king and an enemy slider share a line,
// which squares would something have to occupy to stand between
// them".
package board

// squaresBetween[a][b] is the bitboard of squares STRICTLY between a
// and b — neither endpoint included — if a and b share a rank, file,
// or diagonal. Empty (0) for any other pair, including a == b and
// pairs that are already adjacent (nothing can be strictly between
// two adjacent squares).
var squaresBetween [64][64]Bitboard

func init() {
	for a := 0; a < 64; a++ {
		for b := 0; b < 64; b++ {
			squaresBetween[a][b] = computeSquaresBetween(Square(a), Square(b))
		}
	}
}

func computeSquaresBetween(a, b Square) Bitboard {
	if a == b {
		return 0
	}
	af, ar := a.File(), a.Rank()
	bf, br := b.File(), b.Rank()
	df, dr := stepSign(bf-af), stepSign(br-ar)

	// Same rank (dr==0), same file (df==0), or same diagonal (equal
	// absolute step in both directions) — anything else shares none
	// of the three lines a slider (or this table) cares about.
	if !(dr == 0 || df == 0 || intAbs(bf-af) == intAbs(br-ar)) {
		return 0
	}

	var between Bitboard
	f, r := af+df, ar+dr
	for f != bf || r != br {
		between.Set(MakeSquare(f, r))
		f += df
		r += dr
	}
	return between
}

// stepSign/intAbs: tiny local helpers, deliberately not named
// sign/abs to avoid reading like a claim on those very common names
// package-wide — this file only needs single-step direction and
// magnitude for board coordinates, nothing more general.
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

func intAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// SquaresBetween returns the bitboard of squares strictly between a
// and b (see squaresBetween's doc comment) — the exported accessor to
// the table above, for packages like movegen that need this geometry
// but can't reach an unexported package-level table directly.
func SquaresBetween(a, b Square) Bitboard {
	return squaresBetween[a][b]
}
