// Package uci implements enough of the Universal Chess Interface
// protocol (https://www.chessprogramming.org/UCI) to let the engine be
// driven from a GUI like Arena or CuteChess, or from any script that
// talks UCI over stdin/stdout.
//
// Supported commands:
//
//	uci          -> "id name ...", "id author ...", "uciok"
//	isready      -> "readyok"
//	ucinewgame   -> reset to the starting position
//	position ... -> set up a position (startpos or fen) plus a move list
//	go ...       -> search and respond "bestmove <move>"
//	quit         -> stop the loop
//
// "go" without an explicit "depth" uses the fixed depth Loop was given
// (mirroring -depth in the CLI's -play mode) — there's no time-control
// logic yet (wtime/btime/movetime/infinite are accepted and ignored),
// that's project step 6. Any other unrecognized command (setoption,
// stop, ponderhit, debug, ...) is silently ignored per the UCI spec,
// rather than treated as an error, so a GUI sending extra commands
// this engine doesn't yet support can't break the session.
package uci

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yourname/dyanis-chess-engine/internal/board"
	"github.com/yourname/dyanis-chess-engine/internal/book"
	"github.com/yourname/dyanis-chess-engine/internal/movegen"
	"github.com/yourname/dyanis-chess-engine/internal/search"
)

const (
	engineName = "Dyanis"
	// TODO: put your own name here before publishing this under UCI —
	// "id author" is meant to identify a real person/team, not a
	// placeholder.
	engineAuthor = "Your Name"
)

// session holds the state a UCI conversation accumulates between
// commands: the current position, the fixed search depth (Loop's
// depth parameter, overridable per-"go" via "go depth N"), and the
// opening book (may be nil).
type session struct {
	pos   *board.Board
	depth int
	book  *book.Book
	w     *bufio.Writer
}

// Loop reads UCI commands from r, one per line, and writes responses
// to w, until "quit" is received or r hits EOF/an error. depth is the
// fixed search depth used for "go" (and "go depth N" without a valid
// N); bk may be nil to play without an opening book. Loop starts from
// the standard starting position; "position" changes that.
func Loop(r *bufio.Reader, w *bufio.Writer, depth int, bk *book.Book) {
	s := &session{pos: board.NewInitialBoard(), depth: depth, book: bk, w: w}

	for {
		line, readErr := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" && !s.handle(line) {
			return
		}
		if readErr != nil {
			return // stdin closed or errored: behave like "quit"
		}
	}
}

// handle dispatches one command line. It returns false when the
// session should stop (i.e. "quit" was received).
func (s *session) handle(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return true
	}

	switch fields[0] {
	case "uci":
		fmt.Fprintf(s.w, "id name %s\n", engineName)
		fmt.Fprintf(s.w, "id author %s\n", engineAuthor)
		fmt.Fprint(s.w, "uciok\n")
		s.w.Flush()

	case "isready":
		fmt.Fprint(s.w, "readyok\n")
		s.w.Flush()

	case "ucinewgame":
		s.pos = board.NewInitialBoard()

	case "position":
		s.handlePosition(fields[1:])

	case "go":
		s.handleGo(fields[1:])

	case "quit":
		return false
	}
	return true
}

// handlePosition implements:
//
//	position startpos [moves m1 m2 ...]
//	position fen <6 FEN fields> [moves m1 m2 ...]
//
// Moves are matched against the position's own legal move list by
// exact coordinate-notation string ("e2e4", "e7e8q" for promotion) —
// that's the format UCI GUIs send and the same format board.Move
// already formats itself as, so no separate parser is needed here
// (unlike SAN input in the CLI's -play, which is a human-typing
// convenience UCI doesn't need).
func (s *session) handlePosition(args []string) {
	if len(args) == 0 {
		return
	}

	var pos *board.Board
	i := 0

	switch args[0] {
	case "startpos":
		pos = board.NewInitialBoard()
		i = 1

	case "fen":
		var fenFields []string
		i = 1
		for i < len(args) && args[i] != "moves" {
			fenFields = append(fenFields, args[i])
			i++
		}
		p, err := board.FromFEN(strings.Join(fenFields, " "))
		if err != nil {
			return // malformed "position" from the GUI: ignore, keep old state
		}
		pos = p

	default:
		return
	}

	if i < len(args) && args[i] == "moves" {
		for _, token := range args[i+1:] {
			m, ok := findMoveByCoordinates(pos, token)
			if !ok {
				return // stop applying at the first move that doesn't match
			}
			pos = pos.MakeMove(m)
		}
	}

	s.pos = pos
}

func findMoveByCoordinates(pos *board.Board, token string) (board.Move, bool) {
	for _, m := range movegen.GenerateLegalMoves(pos) {
		if m.String() == token {
			return m, true
		}
	}
	return board.Move{}, false
}

// handleGo implements "go [depth N] [movetime N] [... other params,
// ignored]" and replies with "bestmove <move>". If bk has a book move
// for the current position, that's played instead of running search
// at all — same book-first behavior as the CLI's -play, via
// search.BestMoveWithBook / search.BestMoveTimedWithBook.
//
// If movetime is given (milliseconds), this uses iterative deepening
// with that as a time budget, with depth (explicit "depth N" if also
// given, else the session default) as the max-depth ceiling — the
// same relationship -movetime/-depth have in the CLI's -play. Without
// movetime, it's a fixed-depth search as before. wtime/btime/winc/
// binc/infinite aren't implemented (full clock-based time allocation
// is a further step beyond this project's current "iterative
// deepening + a flat per-move budget" scope) and are silently ignored.
func (s *session) handleGo(args []string) {
	depth := s.depth
	movetimeMs := 0

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "depth":
			if i+1 < len(args) {
				if d, err := strconv.Atoi(args[i+1]); err == nil {
					depth = d
				}
				i++
			}
		case "movetime":
			if i+1 < len(args) {
				if ms, err := strconv.Atoi(args[i+1]); err == nil {
					movetimeMs = ms
				}
				i++
			}
		}
	}

	var m board.Move
	var err error
	if movetimeMs > 0 {
		m, _, err = search.BestMoveTimedWithBook(s.pos, depth, time.Duration(movetimeMs)*time.Millisecond, s.book)
	} else {
		m, _, err = search.BestMoveWithBook(s.pos, depth, s.book)
	}
	if err != nil {
		return // no legal move (checkmate/stalemate): nothing sensible to send
	}
	fmt.Fprintf(s.w, "bestmove %s\n", m)
	s.w.Flush()
}
