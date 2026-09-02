package uci

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/yourname/dyanis-chess-engine/internal/board"
	"github.com/yourname/dyanis-chess-engine/internal/book"
)

// encode mirrors Polyglot's 16-bit move packing (same scheme used in
// book_test.go and search's opening_book_test.go), for building
// synthetic book entries without hand-computing bit patterns.
func encode(fromFile, fromRank, toFile, toRank int, promo uint16) uint16 {
	return uint16(toFile) | uint16(toRank)<<3 | uint16(fromFile)<<6 | uint16(fromRank)<<9 | promo<<12
}

func run(t *testing.T, input string, depth int, bk *book.Book) string {
	t.Helper()
	var out bytes.Buffer
	Loop(bufio.NewReader(strings.NewReader(input)), bufio.NewWriter(&out), depth, bk)
	return out.String()
}

func TestUCIHandshake(t *testing.T) {
	got := run(t, "uci\nquit\n", 3, nil)
	if !strings.Contains(got, "id name") || !strings.Contains(got, "id author") || !strings.Contains(got, "uciok") {
		t.Errorf("expected a full uci handshake, got %q", got)
	}
}

func TestIsReady(t *testing.T) {
	got := run(t, "isready\nquit\n", 3, nil)
	if !strings.Contains(got, "readyok") {
		t.Errorf("expected readyok, got %q", got)
	}
}

func TestPositionStartposMovesThenGo(t *testing.T) {
	got := run(t, "position startpos moves e2e4 e7e5\ngo depth 1\nquit\n", 3, nil)
	if !strings.Contains(got, "bestmove ") {
		t.Errorf("expected a bestmove line, got %q", got)
	}
}

func TestPositionFEN(t *testing.T) {
	got := run(t, "position fen 4k3/8/8/8/8/8/8/4K2R w K - 0 1 moves\ngo depth 1\nquit\n", 2, nil)
	if !strings.Contains(got, "bestmove ") {
		t.Errorf("expected a bestmove line, got %q", got)
	}
}

func TestGoDepthOverridesSessionDefault(t *testing.T) {
	got := run(t, "position startpos\ngo depth 2\nquit\n", 5, nil)
	if !strings.Contains(got, "bestmove ") {
		t.Errorf("expected a bestmove line, got %q", got)
	}
}

func TestGoWithoutDepthUsesSessionDefault(t *testing.T) {
	got := run(t, "position startpos\ngo\nquit\n", 1, nil)
	if !strings.Contains(got, "bestmove ") {
		t.Errorf("expected a bestmove line using the default depth, got %q", got)
	}
}

func TestUnknownCommandIsIgnoredNotFatal(t *testing.T) {
	got := run(t, "setoption name Foo value Bar\nisready\nquit\n", 3, nil)
	if !strings.Contains(got, "readyok") {
		t.Errorf("unknown command should be ignored, expected readyok afterward, got %q", got)
	}
}

func TestGoPrefersBookMove(t *testing.T) {
	start := board.NewInitialBoard()
	bk := book.New([]book.Entry{
		{Key: start.Hash(), Move: encode(4, 1, 4, 3, 0), Weight: 10}, // e2e4
	})

	got := run(t, "position startpos\ngo depth 3\nquit\n", 3, bk)
	if !strings.Contains(got, "bestmove e2e4") {
		t.Errorf("expected the book move e2e4, got %q", got)
	}
}

func TestQuitStopsTheLoop(t *testing.T) {
	// If "quit" didn't stop the loop, this would hang reading past
	// EOF instead of returning, and the test would time out.
	got := run(t, "quit\nisready\n", 3, nil)
	if strings.Contains(got, "readyok") {
		t.Error("commands after quit should never be processed")
	}
}

func TestGoMovetimeUsesIterativeDeepening(t *testing.T) {
	got := run(t, "position startpos\ngo movetime 200\nquit\n", 5, nil)
	if !strings.Contains(got, "bestmove ") {
		t.Errorf("expected a bestmove line from a movetime-based search, got %q", got)
	}
}
