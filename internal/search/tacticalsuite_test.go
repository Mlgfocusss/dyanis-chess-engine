package search

import (
	"testing"
	"time"

	"github.com/yourname/dyanis-chess-engine/internal/board"
)

// tacticalPosition is one entry of the minimal tactical test suite —
// see plan.md's "Измерение силы, не только новых техник": a FEN plus
// the accepted best move(s) in UCI (from-to[promotion]) notation.
// Several entries accept more than one move where the source itself
// lists more than one as correct (near-ties the suite's original
// author judged equally winning).
type tacticalPosition struct {
	id    string
	fen   string
	moves []string // a pass if BestMove's move matches ANY of these
}

// tacticalSuite is a small, fixed subset of the classic "Win At
// Chess" 300-position set. FEN and best-move fields were taken
// directly from a public WAC.epd-derived source in UCI coordinate
// notation, not retyped from memory or converted by hand from SAN —
// see the chat history for the source. This is NOT a training set:
// nothing here ever feeds back into eval weights or search
// parameters, and nobody tunes anything to make these numbers go up.
// It exists purely as a fast, repeatable THERMOMETER — run it before
// and after a change and compare the percentage solved. A drop is a
// warning sign worth explaining even when node counts or speed
// improved (see the countermove-heuristic episode in plan.md for
// exactly why measuring nodes alone isn't enough).
var tacticalSuite = []tacticalPosition{
	{"WAC001", "2rr3k/pp3pp1/1nnqbN1p/3pN3/2pP4/2P3Q1/PPB4P/R4RK1 w - -", []string{"g3g6"}},
	{"WAC002", "8/7p/5k2/5p2/p1p2P2/Pr1pPK2/1P1R3P/8 b - -", []string{"b3b2"}},
	{"WAC003", "5rk1/1ppb3p/p1pb4/6q1/3P1p1r/2P1R2P/PP1BQ1P1/5RKN w - -", []string{"e3g3"}},
	{"WAC004", "r1bq2rk/pp3pbp/2p1p1pQ/7P/3P4/2PB1N2/PP3PPR/2KR4 w - -", []string{"h6h7"}},
	{"WAC005", "5k2/6pp/p1qN4/1p1p4/3P4/2PKP2Q/PP3r2/3R4 b - -", []string{"c6c4"}},
	{"WAC006", "7k/p7/1R5K/6r1/6p1/6P1/8/8 w - -", []string{"b6b7"}},
	{"WAC008", "r4q1k/p2bR1rp/2p2Q1N/5p2/5p2/2P5/PP3PPP/R5K1 w - -", []string{"e7f7"}},
	{"WAC009", "3q1rk1/p4pp1/2pb3p/3p4/6Pr/1PNQ4/P1PB1PP1/4RRK1 b - -", []string{"d6h2"}},
	{"WAC011", "r1b1kb1r/3q1ppp/pBp1pn2/8/Np3P2/5B2/PPP3PP/R2Q1RK1 w kq -", []string{"f3c6"}},
	{"WAC013", "5rk1/pp4p1/2n1p2p/2Npq3/2p5/6P1/P3P1BP/R4Q1K w - -", []string{"f1f8"}},
	{"WAC014", "r2rb1k1/pp1q1p1p/2n1p1p1/2bp4/5P2/PP1BPR1Q/1BPN2PP/R5K1 w - -", []string{"h3h7"}},
	{"WAC018", "R7/P4k2/8/8/8/8/r7/6K1 w - -", []string{"a8h8"}},
	{"WAC019", "r1b2rk1/ppbn1ppp/4p3/1QP4q/3P4/N4N2/5PPP/R1B2RK1 w - -", []string{"c5c6"}},
	{"WAC022", "r1bqk2r/ppp1nppp/4p3/n5N1/2BPp3/P1P5/2P2PPP/R1BQK2R w KQkq -", []string{"c4a2", "g5f7"}},
	{"WAC027", "7k/pp4np/2p3p1/3pN1q1/3P4/Q7/1r3rPP/2R2RK1 w - -", []string{"a3f8"}},
	{"WAC030", "1r3r2/4q1kp/b1pp2p1/5p2/pPn1N3/6P1/P3PPBP/2QRR1K1 w - -", []string{"e4d6"}},
	{"WAC035", "r3r2k/2R3pp/pp1q1p2/8/3P3R/7P/PP3PP1/3Q2K1 w - -", []string{"h4h7"}},
	{"WAC041", "1k6/5RP1/1P6/1K6/6r1/8/8/8 w - -", []string{"b5a5", "b5c5"}},
	{"WAC050", "k4r2/1R4pb/1pQp1n1p/3P4/5p1P/3P2P1/r1q1R2K/8 w - -", []string{"b7b6"}},
	{"WAC057", "r3q1kr/ppp5/3p2pQ/8/3PP1b1/5R2/PPP3P1/5RK1 w - -", []string{"f3f8"}},
	{"WAC060", "rn1qr1k1/1p2np2/2p3p1/8/1pPb4/7Q/PB1P1PP1/2KR1B1R w - -", []string{"h3h8"}},
}

// tacticalSuiteTimeBudget is how long BestMoveTimed gets per
// position — a fixed time budget rather than a fixed depth so the
// comparison stays meaningful as search speed changes across future
// steps (razoring, delta pruning, etc. all changed how deep a given
// depth number actually searches; a fixed time budget sidesteps
// that). 2s keeps the whole 21-position suite under a minute.
const tacticalSuiteTimeBudget = 2 * time.Second

// tacticalSuiteMaxDepth is a depth ceiling passed to BestMoveTimed
// alongside the time budget — high enough that the time budget is
// almost always what actually stops the search, not this.
const tacticalSuiteMaxDepth = 30

// TestTacticalSuite runs the suite and reports a solved percentage.
// Deliberately never calls t.Error/t.Fatal for an individual miss:
// this is a measurement to read (run with `go test -v -run
// TestTacticalSuite`), not a pass/fail gate — a real engine at a
// fixed shallow time budget is not expected to solve 100% of a
// tactics suite, and a change that drops the percentage a little
// isn't automatically a bug. What matters is watching the number
// move across changes, the same way cmd/profile's node counts are
// watched.
func TestTacticalSuite(t *testing.T) {
	solved := 0
	for _, tc := range tacticalSuite {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			b, err := board.FromFEN(tc.fen)
			if err != nil {
				t.Fatalf("FEN parse failed for %s: %v", tc.id, err)
			}

			m, err := BestMoveTimed(b, tacticalSuiteMaxDepth, tacticalSuiteTimeBudget)
			if err != nil {
				t.Fatalf("BestMoveTimed failed for %s: %v", tc.id, err)
			}

			got := m.String()
			pass := false
			for _, want := range tc.moves {
				if got == want {
					pass = true
					break
				}
			}
			if pass {
				solved++
				t.Logf("%s: PASS (played %s)", tc.id, got)
			} else {
				t.Logf("%s: MISS (wanted one of %v, played %s)", tc.id, tc.moves, got)
			}
		})
	}

	t.Logf("tactical suite: %d/%d solved (%.1f%%)", solved, len(tacticalSuite),
		100*float64(solved)/float64(len(tacticalSuite)))
}
