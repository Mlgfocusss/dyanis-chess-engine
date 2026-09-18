// Command cli is the local, no-UI entry point for developing and
// testing the engine (project step 5's precursor). It can print a
// position, run perft, and play an interactive game against the
// engine's search (-play), optionally backed by a Polyglot opening
// book (-book). Moves in -play are shown and can be typed in Standard
// Algebraic Notation (Nf3, exd5, O-O, e8=Q#, ...); coordinate notation
// (g1f3) still works too, since board.Move.String() and the book
// decoder use it internally. Once the UCI package (step 5) exists,
// this will grow a "uci" subcommand that hands off to uci.Loop over
// stdin/stdout so the engine can be driven from Arena/CuteChess too.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yourname/dyanis-chess-engine/internal/board"
	"github.com/yourname/dyanis-chess-engine/internal/book"
	"github.com/yourname/dyanis-chess-engine/internal/movegen"
	"github.com/yourname/dyanis-chess-engine/internal/perft"
	"github.com/yourname/dyanis-chess-engine/internal/search"
	"github.com/yourname/dyanis-chess-engine/internal/uci"
)

func main() {
	fen := flag.String("fen", board.StartFEN, "FEN of the position to load")
	perftDepth := flag.Int("perft", 0, "if > 0, run perft to this depth and print the result (and per-move breakdown)")
	play := flag.Bool("play", false, "play an interactive game in the terminal against the engine")
	uciMode := flag.Bool("uci", false, "run as a UCI engine over stdin/stdout, for GUIs like Arena or CuteChess, instead of the local CLI")
	depth := flag.Int("depth", 3, "search depth in plies for -play/-uci. Fixed depth by default; if -movetime is also set, this becomes the max depth ceiling for iterative deepening instead")
	movetime := flag.Int("movetime", 0, "time budget in milliseconds for -play's engine moves; 0 (default) means fixed-depth search instead of iterative deepening with a timer")
	bookPath := flag.String("book", "assets/gm2001.bin", "comma-separated list of Polyglot .bin opening books, in priority order: while the position is found in the first book, -play/-uci plays from it; only checks the second book if the first has no entry for that position, and so on (see book.Chain). E.g. -book=\"assets/gm2001.bin,assets/komodo.bin,assets/performance.bin\". Pass -book=\"\" to disable. A book that fails to load only prints a warning and is skipped — never a hard failure, so the default doesn't break -play on a fresh checkout that hasn't downloaded any .bin yet")
	flag.Parse()

	b, err := board.FromFEN(*fen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid FEN: %v\n", err)
		os.Exit(1)
	}

	bk := loadBookChain(*bookPath, os.Stderr)
	if bk == nil && *bookPath != "" {
		fmt.Fprintf(os.Stderr, "note: playing without an opening book (none of %q loaded)\n", *bookPath)
	}

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()

	if *uciMode {
		uci.Loop(bufio.NewReader(os.Stdin), w, *depth, bk)
		return
	}

	if *play {
		playInteractive(b, w, *depth, *movetime, bk)
		return
	}

	fmt.Fprint(w, b.String())
	fmt.Fprintf(w, "side to move: %s\n", b.SideToMove)
	fmt.Fprintf(w, "FEN: %s\n", b.ToFEN())

	if *perftDepth > 0 {
		fmt.Fprintf(w, "\nperft(%d):\n", *perftDepth)
		total := uint64(0)
		for move, count := range perft.Divide(b, *perftDepth) {
			fmt.Fprintf(w, "  %s: %d\n", move, count)
			total += count
		}
		fmt.Fprintf(w, "total: %d\n", total)
	}
}

// loadBookChain parses the -book flag's comma-separated list of
// paths and loads each one, in order, into a book.Source suitable for
// search.BestMoveWithBook: nil if the list is empty (-book="",
// opening book disabled) or every path failed to load; the lone
// *book.Book directly if exactly one loaded (no point wrapping a
// single book in a Chain); a *book.Chain, tried in list order, if
// two or more loaded.
//
// A path that fails to load (missing file, corrupt data, ...) only
// prints a warning to warnOut and is skipped — same "never a hard
// failure" behavior the original single-book version had, just
// per-path now instead of all-or-nothing: e.g. a typo'd komodo.bin
// shouldn't also cost you a perfectly good gm2001.bin.
func loadBookChain(bookFlag string, warnOut *os.File) book.Source {
	var books []*book.Book
	for _, path := range strings.Split(bookFlag, ",") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue // handles both -book="" and stray ",," typos
		}
		bk, err := book.Load(path)
		if err != nil {
			fmt.Fprintf(warnOut, "warning: could not load book %q: %v — skipping\n", path, err)
			continue
		}
		books = append(books, bk)
	}

	switch len(books) {
	case 0:
		return nil // literal nil book.Source: no book loaded, play from search only
	case 1:
		return books[0]
	default:
		return book.NewChain(books...)
	}
}

// playInteractive runs a text-based game loop: type moves in SAN
// ("e4", "Nf3", "O-O", "exd5", "e8=Q#" — coordinate notation like
// "e2e4" also still works) and the engine replies. If bk is non-nil
// and the current position is in the book, the engine plays a book
// move (weighted at random by how often it was actually played)
// instead of running search. Every move played, by either side, is
// recorded in SAN and printed as standard movetext once the game ends.
//
// If movetimeMs > 0, the engine's moves use iterative deepening with
// that many milliseconds as a time budget (depth becomes the max-depth
// ceiling for that search rather than a fixed depth) via
// search.BestMoveTimedWithBook; otherwise it's a fixed-depth search via
// search.BestMoveWithBook, as before.
func playInteractive(b *board.Board, w *bufio.Writer, depth, movetimeMs int, bk book.Source) {
	reader := bufio.NewReader(os.Stdin)
	var sanMoves []string

	// history holds the Zobrist hash of every position reached in this
	// game so far, INCLUDING the current one (b.Hash() below covers
	// the starting position; each MakeMove call further down appends
	// the resulting position's hash right after playing it). This is
	// exactly what GameStatusWithHistory needs to detect threefold
	// repetition — see movegen/status.go's doc comment on that
	// function for the exact contract.
	history := []uint64{b.Hash()}

	printLog := func() {
		if len(sanMoves) > 0 {
			fmt.Fprintf(w, "\n%s\n", movegen.GameLog(sanMoves))
		}
	}

	for {
		fmt.Fprint(w, b.String())
		fmt.Fprintf(w, "side to move: %s\n", b.SideToMove)
		w.Flush()

		switch movegen.GameStatusWithHistory(b, history) {
		case movegen.Checkmate:
			winner := b.SideToMove.Opposite()
			fmt.Fprintf(w, "Checkmate. %s wins.\n", winner)
			printLog()
			return
		case movegen.Stalemate:
			fmt.Fprintf(w, "Stalemate. Draw.\n")
			printLog()
			return
		case movegen.DrawFiftyMove:
			fmt.Fprintf(w, "Draw by the 50-move rule.\n")
			printLog()
			return
		case movegen.DrawRepetition:
			fmt.Fprintf(w, "Draw by threefold repetition.\n")
			printLog()
			return
		}

		legal := movegen.GenerateLegalMoves(b)

		var m board.Move
		fromBook := false

		if b.SideToMove == board.White {
			fmt.Fprint(w, "your move (SAN, e.g. e4, Nf3, O-O; coordinates like e2e4 also work): ")
			w.Flush()
			line, _ := reader.ReadString('\n')
			line = strings.TrimSpace(line)

			parsed, ok := parseUserMove(b, legal, line)
			if !ok {
				fmt.Fprintf(w, "not a legal move: %q\n", line)
				continue
			}
			m = parsed
		} else {
			var engineMove board.Move
			var isFromBook bool
			var err error
			if movetimeMs > 0 {
				engineMove, isFromBook, err = search.BestMoveTimedWithBook(b, depth, time.Duration(movetimeMs)*time.Millisecond, bk)
			} else {
				engineMove, isFromBook, err = search.BestMoveWithBook(b, depth, bk)
			}
			if err != nil {
				fmt.Fprintf(w, "engine error: %v\n", err)
				printLog()
				return
			}
			m, fromBook = engineMove, isFromBook
		}

		san := movegen.SAN(b, m) // must be computed BEFORE MakeMove: SAN needs the "before" position
		sanMoves = append(sanMoves, san)

		if b.SideToMove == board.Black {
			if fromBook {
				fmt.Fprintf(w, "engine plays (book): %s\n", san)
			} else {
				fmt.Fprintf(w, "engine plays: %s\n", san)
			}
		} else {
			fmt.Fprintf(w, "you played: %s\n", san)
		}

		b.MakeMove(m)
		history = append(history, b.Hash())
	}
}

// parseUserMove accepts either coordinate notation ("e2e4", matched
// directly against Move.String() as before) or Standard Algebraic
// Notation ("e4", "Nf3", "O-O", ...) via movegen.ParseSAN. Coordinate
// notation is tried first since it's an exact, unambiguous string
// match with no parsing involved.
func parseUserMove(b *board.Board, legal []board.Move, input string) (board.Move, bool) {
	trimmed := strings.TrimSpace(input)

	for _, m := range legal {
		if strings.EqualFold(m.String(), trimmed) {
			return m, true
		}
	}

	m, err := movegen.ParseSAN(b, trimmed)
	if err != nil {
		return board.Move{}, false
	}
	return m, true
}
