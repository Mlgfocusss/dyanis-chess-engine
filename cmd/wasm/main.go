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

// game holds all mutable state for one browser-side session. A wasm
// module is loaded once per page and this program never exits (main
// blocks forever on select{}), so package-level state here plays the
// same role uci.session did for the UCI loop — except there's only
// ever one "session", the page itself.
var game = struct {
	pos      *board.Board
	history  []*board.Board // history[0] is the starting position; used for undo
	sanMoves []string
	bk       *book.Book // nil until Dyanis.loadBook(bytes) is called
}{}

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
	game.history = []*board.Board{b}
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

func buildState() stateResponse {
	b := game.pos

	status := "ongoing"
	switch movegen.GameStatus(b) {
	case movegen.Checkmate:
		status = "checkmate"
	case movegen.Stalemate:
		status = "stalemate"
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
// nothing to undo (i.e. we're at the starting position).
func jsUndo(this js.Value, args []js.Value) any {
	if len(game.history) <= 1 {
		return toJSON(moveResponse{OK: false, Error: "nothing to undo", State: buildState()})
	}
	game.history = game.history[:len(game.history)-1]
	game.pos = game.history[len(game.history)-1]
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

// Dyanis.loadBook(bytes) -> {"ok":true,"entries":N} or {"ok":false,"error":...}.
// bytes must be a Uint8Array holding the raw contents of a Polyglot
// .bin file — there's no filesystem in the browser, so JS has to
// fetch() the file itself and hand the bytes over, e.g.:
//
//	const buf = await (await fetch("assets/gm2001.bin")).arrayBuffer();
//	log(Dyanis.loadBook(new Uint8Array(buf)));
//
// Once loaded, engineMove automatically checks the book first, same
// as -play/-uci in the CLI — no separate "use book" flag needed.
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
	game.bk = bk
	return toJSON(map[string]any{"ok": true, "entries": bk.Len()})
}

// Dyanis.clearBook() -> {"ok":true}. Goes back to pure search, same
// as -book="" in the CLI.
func jsClearBook(this js.Value, args []js.Value) any {
	game.bk = nil
	return toJSON(map[string]any{"ok": true})
}

// Dyanis.bookInfo() -> {"loaded":bool,"entries":N}. Lets the frontend
// show book status (e.g. a badge) without having to track it itself
// across page reloads/re-renders.
func jsBookInfo(this js.Value, args []js.Value) any {
	if game.bk == nil {
		return toJSON(map[string]any{"loaded": false, "entries": 0})
	}
	return toJSON(map[string]any{"loaded": true, "entries": game.bk.Len()})
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
// records it into game state: the new position, undo history, and
// SAN move log. san is computed BEFORE MakeMove, same as the CLI's
// -play — SAN needs the "before" position to render correctly.
func applyMove(b *board.Board, m board.Move) {
	san := movegen.SAN(b, m)
	game.pos = b.MakeMove(m)
	game.history = append(game.history, game.pos)
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
