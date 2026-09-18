//go:build js && wasm

// Command wasm compiles the engine to WebAssembly and exposes a small
// JS-facing API via syscall/js, meant to be driven from a browser
// (project step 7). It deliberately does NOT reuse internal/uci — UCI
// is a text protocol for engine<->GUI processes talking over
// stdin/stdout; a JS<->Go boundary inside the same page is a plain
// function-call boundary, so this exposes typed(ish) JSON in/out
// functions instead of speaking UCI text back and forth with
// JavaScript.
//
// All exposed functions are attached under a single `window.Dyanis`
// object (see registerAPI) rather than polluting the global
// namespace with a function per operation. Every function returns a
// JSON string (parse it on the JS side); see wasm/index.html for a
// minimal smoke-test page that exercises the whole API.
//
// Opening-book support: since book.Load reads from a file path via
// os.Open (no filesystem in the browser), the wasm side instead
// exposes Dyanis.loadBook(bytes) — JS fetch()es a .bin and hands the
// raw bytes to Go, which decodes them with book.Parse. See jsLoadBook
// below. game.bk starts out nil (playing without a book) until/unless
// loadBook is called.
package main

import (
	"encoding/json"
	"strings"
	"syscall/js"
	"time"

	"github.com/yourname/dyanis-chess-engine/internal/board"
	"github.com/yourname/dyanis-chess-engine/internal/book"
	"github.com/yourname/dyanis-chess-engine/internal/movegen"
	"github.com/yourname/dyanis-chess-engine/internal/perft"
	"github.com/yourname/dyanis-chess-engine/internal/search"
)

// moveRecord is one played move plus what UnmakeMove needs to reverse
// it — exactly the (Move, Undo) pair board.Board.MakeMove/UnmakeMove
// are built around (see internal/board/move.go). Undo alone isn't
// enough on its own; UnmakeMove needs the original Move too.
type moveRecord struct {
	move board.Move
	undo board.Undo
}

// game holds all mutable state for one browser-side session. A wasm
// module is loaded once per page and this program never exits (main
// blocks forever on select{}), so package-level state here plays the
// same role uci.session did for the UCI loop — except there's only
// ever one "session", the page itself.
//
// There is exactly ONE live *board.Board for the whole game: pos.
// Every played move mutates it in place via MakeMove (see
// internal/board/move.go's make/unmake design) rather than producing
// a new Board — so, unlike the old copy-per-move version of this
// file, undo history can no longer be "an array of board snapshots"
// (every entry would just be the same mutating pointer, silently
// rewritten out from under you by the next move). Instead, moves
// tracks one (Move, Undo) pair per ply played; jsUndo reverses the
// most recent one with pos.UnmakeMove(rec.move, rec.undo) rather than
// switching which *Board is "current". hashes is kept as its own
// parallel slice (rather than re-deriving it from moves on every call)
// purely because GameStatusWithHistory wants []uint64 directly, and
// walking pos forward/back through moves just to re-collect hashes on
// every buildState call would be wasted work for no benefit over
// just appending/trimming one more slice alongside moves.
var game = struct {
	pos      *board.Board
	moves    []moveRecord // one entry per ply played so far; used by jsUndo
	hashes   []uint64     // hashes[0] = starting position; hashes[i+1] = position after moves[i]
	sanMoves []string

	// books holds every book loaded so far, in the order loadBook was
	// called — first loaded = highest priority (matches CLI's -book
	// flag order). bk is the resulting book.Source handed to search:
	// nil while books is empty, a lone *book.Book after one load (no
	// point wrapping a single book in a Chain), or a *book.Chain once
	// there are two or more. Rebuilt by rebuildBookSource after every
	// loadBook/clearBook call rather than computed on the fly, so
	// engineMove/searchMove never pay chain-construction cost per move.
	books []*book.Book
	bk    book.Source
}{}

// rebuildBookSource recomputes game.bk from game.books. Called after
// every change to game.books (load or clear) so game.bk is always
// ready to hand straight to search.BestMoveWithBook.
func rebuildBookSource() {
	switch len(game.books) {
	case 0:
		game.bk = nil // literal nil book.Source — see opening_book.go's caller-beware note
	case 1:
		game.bk = game.books[0]
	default:
		game.bk = book.NewChain(game.books...)
	}
}

func resetGame(startFEN string) error {
	var b *board.Board
	if startFEN == "" {
		b = board.NewInitialBoard()
	} else {
		parsed, err := board.FromFEN(startFEN)
		if err != nil {
			return err
		}
		b = parsed
	}
	game.pos = b
	game.moves = nil
	game.hashes = []uint64{b.Hash()}
	game.sanMoves = nil
	return nil
}

func main() {
	if err := resetGame(""); err != nil {
		panic(err) // the starting position must always parse
	}
	registerAPI()
	notifyReady()
	select {} // keep the wasm program alive; all work happens via JS callbacks
}

// notifyReady calls window.resolveDyanisReady(), if the host page
// defined it, so JS knows window.Dyanis is now safe to use.
// wasm_exec.js's go.run() returns a Promise that only resolves when
// main() returns — which for us is never (see select{} above) — so
// JS can't just await go.run() to know when we're ready. This
// explicit signal is the alternative. Safe to no-op if the page
// didn't set the hook up (e.g. this wasm module loaded on a
// different host page/test harness that doesn't need it) — which is
// also why registerAPI() above still runs and window.Dyanis still
// gets populated regardless: that part never depended on this hook.
func notifyReady() {
	fn := js.Global().Get("resolveDyanisReady")
	if fn.Type() == js.TypeFunction {
		fn.Invoke()
	}
}

// --- JSON response shapes -------------------------------------------

type moveInfo struct {
	UCI string `json:"uci"` // coordinate notation, e.g. "e2e4" or "e7e8q" for promotion
	SAN string `json:"san"` // e.g. "e4", "Nf3", "O-O", "e8=Q#"
}

// stateResponse is a full snapshot of the current position — enough
// for a frontend to render the board and know what's legal without a
// second round-trip.
type stateResponse struct {
	FEN            string     `json:"fen"`
	SideToMove     string     `json:"sideToMove"` // "white" | "black"
	Status         string     `json:"status"`     // "ongoing" | "checkmate" | "stalemate"
	InCheck        bool       `json:"inCheck"`
	LegalMoves     []moveInfo `json:"legalMoves"`
	Log            string     `json:"log"` // e.g. "1. e4 c5 2. Nf3 Nc6"
	FullmoveNumber int        `json:"fullmoveNumber"`
}

// moveResponse wraps the result of any call that changes the
// position (makeMove, engineMove, undo): whether it succeeded, what
// move was played (if any), and the resulting state either way, so
// the frontend can always re-render from one response.
type moveResponse struct {
	OK       bool          `json:"ok"`
	Error    string        `json:"error,omitempty"`
	UCI      string        `json:"uci,omitempty"`
	SAN      string        `json:"san,omitempty"`
	FromBook bool          `json:"fromBook,omitempty"`
	State    stateResponse `json:"state"`
}

// historyHashes returns the Zobrist hash of every position reached so
// far in the game, in order, INCLUDING the current one — exactly what
// GameStatusWithHistory needs for its repetition check (see that
// function's doc comment in internal/movegen/status.go). Just game's
// own hashes slice, kept up to date by applyMove/jsUndo rather than
// re-derived from game.pos on every call — see game's doc comment for
// why a parallel []uint64 replaced walking board snapshots here.
func historyHashes() []uint64 {
	return game.hashes
}

func buildState() stateResponse {
	b := game.pos

	status := "ongoing"
	switch movegen.GameStatusWithHistory(b, historyHashes()) {
	case movegen.Checkmate:
		status = "checkmate"
	case movegen.Stalemate:
		status = "stalemate"
	case movegen.DrawFiftyMove:
		status = "draw-fifty-move"
	case movegen.DrawRepetition:
		status = "draw-repetition"
	}

	legal := movegen.GenerateLegalMoves(b)
	moves := make([]moveInfo, len(legal))
	for i, m := range legal {
		moves[i] = moveInfo{UCI: m.String(), SAN: movegen.SAN(b, m)}
	}

	return stateResponse{
		FEN:            b.ToFEN(),
		SideToMove:     b.SideToMove.String(),
		Status:         status,
		InCheck:        movegen.InCheck(b),
		LegalMoves:     moves,
		Log:            movegen.GameLog(game.sanMoves),
		FullmoveNumber: b.FullmoveNumber,
	}
}

func toJSON(v any) js.Value {
	data, err := json.Marshal(v)
	if err != nil {
		// Marshaling our own known-good struct types shouldn't fail;
		// this is just a defensive fallback so a JS caller never gets
		// back something JSON.parse can't handle.
		data, _ = json.Marshal(map[string]string{"error": err.Error()})
	}
	return js.ValueOf(string(data))
}

// --- JS API -----------------------------------------------------------

func registerAPI() {
	api := js.Global().Get("Object").New()

	api.Set("newGame", js.FuncOf(jsNewGame))
	api.Set("getState", js.FuncOf(jsGetState))
	api.Set("makeMove", js.FuncOf(jsMakeMove))
	api.Set("undo", js.FuncOf(jsUndo))
	api.Set("engineMove", js.FuncOf(jsEngineMove))
	api.Set("perft", js.FuncOf(jsPerft))
	api.Set("loadBook", js.FuncOf(jsLoadBook))
	api.Set("clearBook", js.FuncOf(jsClearBook))
	api.Set("bookInfo", js.FuncOf(jsBookInfo))
	api.Set("searchMove", js.FuncOf(jsSearchMove))

	js.Global().Set("Dyanis", api)
}

// Dyanis.newGame(fen?) -> stateResponse JSON.
// With no argument (or ""), resets to the standard starting position.
func jsNewGame(this js.Value, args []js.Value) any {
	fen := ""
	if len(args) > 0 && args[0].Type() == js.TypeString {
		fen = args[0].String()
	}
	if err := resetGame(fen); err != nil {
		return toJSON(map[string]any{"ok": false, "error": err.Error()})
	}
	return toJSON(buildState())
}

// Dyanis.getState() -> stateResponse JSON, no side effects.
func jsGetState(this js.Value, args []js.Value) any {
	return toJSON(buildState())
}

// Dyanis.makeMove(move) -> moveResponse JSON. move can be SAN ("e4",
// "Nf3", "O-O", "e8=Q") or coordinate notation ("e2e4", "e7e8q").
func jsMakeMove(this js.Value, args []js.Value) any {
	if len(args) < 1 || args[0].Type() != js.TypeString {
		return toJSON(moveResponse{OK: false, Error: `makeMove expects a move string, e.g. "e4" or "e2e4"`, State: buildState()})
	}
	input := args[0].String()
	b := game.pos
	legal := movegen.GenerateLegalMoves(b)

	m, ok := parseMoveInput(b, legal, input)
	if !ok {
		return toJSON(moveResponse{OK: false, Error: "illegal or unrecognized move: " + input, State: buildState()})
	}

	applyMove(b, m)
	return toJSON(moveResponse{OK: true, UCI: m.String(), SAN: game.sanMoves[len(game.sanMoves)-1], State: buildState()})
}

// Dyanis.undo() -> moveResponse JSON. No-op (ok:false) if there's
// nothing to undo (i.e. we're at the starting position). Reverses the
// most recently played ply with pos.UnmakeMove — there's only ever
// the one *board.Board (game.pos); this puts it back to exactly how
// it stood before that move, the same way search.go unwinds its own
// recursion (see move.go's Undo doc comment).
func jsUndo(this js.Value, args []js.Value) any {
	if len(game.moves) == 0 {
		return toJSON(moveResponse{OK: false, Error: "nothing to undo", State: buildState()})
	}
	last := game.moves[len(game.moves)-1]
	game.pos.UnmakeMove(last.move, last.undo)
	game.moves = game.moves[:len(game.moves)-1]
	game.hashes = game.hashes[:len(game.hashes)-1]
	game.sanMoves = game.sanMoves[:len(game.sanMoves)-1]
	return toJSON(moveResponse{OK: true, State: buildState()})
}

// Dyanis.engineMove(depth?, movetimeMs?) -> moveResponse JSON.
// depth defaults to 3. If movetimeMs > 0, uses iterative deepening
// with that time budget (depth becomes the max-depth ceiling)
// instead of a fixed-depth search — same relationship as -depth/
// -movetime in the CLI's -play.
//
// This runs synchronously on the JS main thread: at higher depths or
// longer movetimes it will visibly block the page. Fine for a first
// smoke test; worth moving into a Web Worker once step 8's React
// board is wired up, so the UI can show a "thinking..." state instead
// of freezing.
func jsEngineMove(this js.Value, args []js.Value) any {
	depth := 3
	movetimeMs := 0
	if len(args) > 0 && args[0].Type() == js.TypeNumber {
		depth = args[0].Int()
	}
	if len(args) > 1 && args[1].Type() == js.TypeNumber {
		movetimeMs = args[1].Int()
	}

	b := game.pos
	var m board.Move
	var fromBook bool
	var err error
	if movetimeMs > 0 {
		m, fromBook, err = search.BestMoveTimedWithBook(b, depth, time.Duration(movetimeMs)*time.Millisecond, game.bk)
	} else {
		m, fromBook, err = search.BestMoveWithBook(b, depth, game.bk)
	}
	if err != nil {
		return toJSON(moveResponse{OK: false, Error: err.Error(), State: buildState()})
	}

	applyMove(b, m)
	return toJSON(moveResponse{OK: true, UCI: m.String(), SAN: game.sanMoves[len(game.sanMoves)-1], FromBook: fromBook, State: buildState()})
}

// Dyanis.perft(depth?) -> {"depth":N,"nodes":N} JSON, for verifying
// the wasm build's move generator matches `go run ./cmd/cli -perft N`
// from the current position. depth defaults to 3.
func jsPerft(this js.Value, args []js.Value) any {
	depth := 3
	if len(args) > 0 && args[0].Type() == js.TypeNumber {
		depth = args[0].Int()
	}
	total := uint64(0)
	for _, count := range perft.Divide(game.pos, depth) {
		total += count
	}
	return toJSON(map[string]any{"depth": depth, "nodes": total})
}

// Dyanis.loadBook(bytes) -> {"ok":true,"entries":N,"books":N} or
// {"ok":false,"error":...}. bytes must be a Uint8Array holding the
// raw contents of a Polyglot .bin file — there's no filesystem in the
// browser, so JS has to fetch() the file itself and hand the bytes
// over, e.g.:
//
//	const buf = await (await fetch("assets/gm2001.bin")).arrayBuffer();
//	log(Dyanis.loadBook(new Uint8Array(buf)));
//
// Calling this more than once does NOT replace the previous book —
// it APPENDS to a priority chain (see book.Chain in internal/book):
// the first book loaded is consulted first for any given position;
// later ones are only consulted if earlier ones have no entry for it.
// So to get "try gm2001.bin, then komodo.bin, then performance.bin":
//
//	for (const name of ["gm2001.bin", "komodo.bin", "performance.bin"]) {
//	  const buf = await (await fetch(`assets/${name}`)).arrayBuffer();
//	  Dyanis.loadBook(new Uint8Array(buf));
//	}
//
// Call Dyanis.clearBook() first if you want to replace the chain
// instead of extending it (e.g. the user picked a different book set
// in the UI). "entries" in the response is this call's book alone;
// "books" is the running count of books now in the chain.
//
// Once loaded, engineMove automatically checks the book(s) first,
// same as -play/-uci in the CLI — no separate "use book" flag needed.
func jsLoadBook(this js.Value, args []js.Value) any {
	if len(args) < 1 {
		return toJSON(map[string]any{"ok": false, "error": "loadBook expects a Uint8Array of .bin file contents"})
	}

	data := make([]byte, args[0].Get("length").Int())
	js.CopyBytesToGo(data, args[0])

	bk, err := book.Parse(data)
	if err != nil {
		return toJSON(map[string]any{"ok": false, "error": err.Error()})
	}
	game.books = append(game.books, bk)
	rebuildBookSource()
	return toJSON(map[string]any{"ok": true, "entries": bk.Len(), "books": len(game.books)})
}

// Dyanis.clearBook() -> {"ok":true}. Drops the entire chain and goes
// back to pure search, same as -book="" in the CLI. Call this before
// loadBook if you want to replace the chain rather than extend it.
func jsClearBook(this js.Value, args []js.Value) any {
	game.books = nil
	rebuildBookSource()
	return toJSON(map[string]any{"ok": true})
}

// Dyanis.bookInfo() -> {"loaded":bool,"books":N,"entries":N}.
// "entries" sums every book in the chain — informational only, since
// a position covered by book 1 never actually reaches book 2's
// entries (see book.Chain.Len's doc). Lets the frontend show book
// status (e.g. a badge) without having to track it itself across page
// reloads/re-renders.
func jsBookInfo(this js.Value, args []js.Value) any {
	if len(game.books) == 0 {
		return toJSON(map[string]any{"loaded": false, "books": 0, "entries": 0})
	}
	total := 0
	for _, b := range game.books {
		total += b.Len()
	}
	return toJSON(map[string]any{"loaded": true, "books": len(game.books), "entries": total})
}

// Dyanis.searchMove(fen, depth?, movetimeMs?) -> like engineMove's
// response, but computed from a caller-supplied FEN instead of the
// module's own `game.pos`. Doesn't read or write `game` at all — this
// statelessness is exactly what lets a SECOND copy of this wasm
// module, loaded in a Web Worker, do the actual (potentially slow,
// unbounded-depth) search off the main thread: the worker's instance
// has its own separate memory and has no idea what the main thread's
// `game` currently is, so it needs the position handed to it
// explicitly. The main thread stays responsible for the authoritative
// game state (newGame/makeMove/undo/getState) and applies whatever
// move the worker decides on via its own makeMove, same as it would
// for a human move. See web/public/engine-worker.js and
// web/src/engine/dyanisEngine.js.
//
// Uses this instance's own game.bk for the book check — call loadBook
// on whichever instance (main thread or worker) is actually going to
// call searchMove/engineMove for the book to have any effect there.
func jsSearchMove(this js.Value, args []js.Value) any {
	if len(args) < 1 || args[0].Type() != js.TypeString {
		return toJSON(map[string]any{"ok": false, "error": "searchMove expects a FEN string as the first argument"})
	}
	fen := args[0].String()
	depth := 3
	movetimeMs := 0
	if len(args) > 1 && args[1].Type() == js.TypeNumber {
		depth = args[1].Int()
	}
	if len(args) > 2 && args[2].Type() == js.TypeNumber {
		movetimeMs = args[2].Int()
	}

	b, err := board.FromFEN(fen)
	if err != nil {
		return toJSON(map[string]any{"ok": false, "error": err.Error()})
	}

	var m board.Move
	var fromBook bool
	if movetimeMs > 0 {
		m, fromBook, err = search.BestMoveTimedWithBook(b, depth, time.Duration(movetimeMs)*time.Millisecond, game.bk)
	} else {
		m, fromBook, err = search.BestMoveWithBook(b, depth, game.bk)
	}
	if err != nil {
		return toJSON(map[string]any{"ok": false, "error": err.Error()})
	}

	return toJSON(map[string]any{"ok": true, "uci": m.String(), "san": movegen.SAN(b, m), "fromBook": fromBook})
}

// --- helpers ------------------------------------------------------------

// applyMove plays m (already validated as legal, from position b) and
// records it into game state: the move itself (mutating game.pos, the
// one and only live board, in place), its Undo (so jsUndo can reverse
// it later), the resulting hash, and the SAN move log. san is
// computed BEFORE MakeMove, same as the CLI's -play — SAN needs the
// "before" position to render correctly. b is always game.pos itself
// (every caller passes that in) — b is a plain, un-renamed alias, not
// a separate board, so no reassignment back into game.pos is needed
// after MakeMove the way the old copy-per-move version needed one.
func applyMove(b *board.Board, m board.Move) {
	san := movegen.SAN(b, m)
	undo := b.MakeMove(m)
	game.moves = append(game.moves, moveRecord{move: m, undo: undo})
	game.hashes = append(game.hashes, b.Hash())
	game.sanMoves = append(game.sanMoves, san)
}

// parseMoveInput mirrors cmd/cli's parseUserMove: try an exact
// coordinate-notation match first (unambiguous, no parsing), then
// fall back to SAN.
func parseMoveInput(b *board.Board, legal []board.Move, input string) (board.Move, bool) {
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
