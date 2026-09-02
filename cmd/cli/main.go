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
	bookPath := flag.String("book", "assets/gm2001.bin", "path to a Polyglot .bin opening book; while the position stays in the book, -play plays from it instead of searching. Pass -book=\"\" to disable. If the file can't be loaded, this only prints a warning and plays without a book — it's never a hard failure, since the default shouldn't break -play on a fresh checkout that hasn't downloaded any .bin yet")
	flag.Parse()

	b, err := board.FromFEN(*fen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid FEN: %v\n", err)
		os.Exit(1)
	}

	var bk *book.Book
	if *bookPath != "" {
		bk, err = book.Load(*bookPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not load book %q: %v — playing without an opening book\n", *bookPath, err)
			bk = nil
		}
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
func playInteractive(b *board.Board, w *bufio.Writer, depth, movetimeMs int, bk *book.Book) {
	reader := bufio.NewReader(os.Stdin)
	var sanMoves []string

	printLog := func() {
		if len(sanMoves) > 0 {
			fmt.Fprintf(w, "\n%s\n", movegen.GameLog(sanMoves))
		}
	}

	for {
		fmt.Fprint(w, b.String())
		fmt.Fprintf(w, "side to move: %s\n", b.SideToMove)
		w.Flush()

		status := movegen.GameStatus(b)
		if status == movegen.Checkmate {
			winner := b.SideToMove.Opposite()
			fmt.Fprintf(w, "Checkmate. %s wins.\n", winner)
			printLog()
			return
		}
		if status == movegen.Stalemate {
			fmt.Fprintf(w, "Stalemate. Draw.\n")
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

		b = b.MakeMove(m)
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
