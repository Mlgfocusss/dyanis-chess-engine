package movegen

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/yourname/dyanis-chess-engine/internal/board"
)

// TestPinsNeverAllowExposingTheKing checks Pins() for soundness, not
// completeness: for every pinned piece Pins() reports, every move
// GenerateLegalMoves (already trusted — see the perft suite) actually
// allows for that piece must land inside AllowedTo. If it didn't,
// AllowedTo would be too permissive — exactly the property a future
// generator built to trust Pins() instead of make/unmake would need,
// and exactly the kind of bug that wouldn't show up as a wrong perft
// count from THIS test's own random scatters (they're not fed through
// perft at all, just checked directly against the existing generator).
func TestPinsNeverAllowExposingTheKing(t *testing.T) {
	pieceLetters := []byte("PNBRQpnbrq") // no K/k here — see randomFEN
	rng := rand.New(rand.NewSource(20260919))

	// randomFEN scatters pieces with exactly one king per color, on
	// two distinct random squares, filled in first — everything else
	// is filled in around them from pieceLetters (which deliberately
	// excludes K/k). A second king of the same color would make
	// board.Board.KingSquare's notion of "the" king ambiguous, which
	// is exactly the kind of position real play can never produce
	// (MakeMove never creates or removes a king) and Pins()/Checkers()
	// have no reason to handle — see Pins' own king-blocker guard in
	// movegen.go for the belt-and-suspenders version of this same
	// fix, kept there in case this function is ever called on some
	// other malformed board.
	randomFEN := func(sideToMove byte) string {
		var squares [64]byte // 0 = empty

		whiteKingSq := rng.Intn(64)
		blackKingSq := rng.Intn(64)
		for blackKingSq == whiteKingSq {
			blackKingSq = rng.Intn(64)
		}
		squares[whiteKingSq] = 'K'
		squares[blackKingSq] = 'k'

		for i := range squares {
			if squares[i] != 0 {
				continue // already a king
			}
			if rng.Float64() < 0.35 {
				squares[i] = pieceLetters[rng.Intn(len(pieceLetters))]
			}
		}

		var ranks [8]string
		for r := 0; r < 8; r++ {
			var sb strings.Builder
			for f := 0; f < 8; f++ {
				idx := r*8 + f // matches board.MakeSquare(file, rank) = rank*8+file
				if squares[idx] == 0 {
					sb.WriteByte('1')
				} else {
					sb.WriteByte(squares[idx])
				}
			}
			ranks[7-r] = sb.String()
		}
		return strings.Join(ranks[:], "/") + " " + string(sideToMove) + " - - 0 1"
	}

	const trials = 500
	for trial := 0; trial < trials; trial++ {
		side := byte('w')
		if rng.Intn(2) == 0 {
			side = 'b'
		}
		b, err := board.FromFEN(randomFEN(side))
		if err != nil {
			t.Fatalf("trial %d: FromFEN error: %v", trial, err)
		}
		// A random scatter very often has no king at all for one or
		// both sides. Pins/AttackersTo already handle that (see
		// AttackersTo's NoSquare guard), but there's nothing useful to
		// check when the side to move has none.
		if b.KingSquare(b.SideToMove) == board.NoSquare {
			continue
		}

		legal := GenerateLegalMoves(b)
		for _, pin := range Pins(b) {
			for _, m := range legal {
				if m.From != pin.Square {
					continue
				}
				var to board.Bitboard
				to.Set(m.To)
				if to&pin.AllowedTo == 0 {
					t.Fatalf("trial %d: FEN %s: Pins() restricts %s to %#016x, but the trusted GenerateLegalMoves allows %s — AllowedTo is unsound",
						trial, b.ToFEN(), pin.Square, uint64(pin.AllowedTo), m)
				}
			}
		}
	}
}
