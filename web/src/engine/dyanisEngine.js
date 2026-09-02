// Two wasm instances are involved here, deliberately:
//
//  1. The MAIN THREAD's own window.Dyanis (loaded by initEngine below)
//     — handles all game bookkeeping: newGame, makeMove, undo,
//     getState. These are cheap (no search), so keeping them
//     synchronous on the main thread is fine and actually desirable —
//     see the sync-vs-async comment lower down.
//
//  2. A SEPARATE instance running inside engine-worker.js, its own
//     thread with its own memory, used ONLY for search (engineMoveAsync
//     below). That's what keeps the page responsive no matter how long
//     a search takes (unbounded depth included) — the expensive work
//     never runs on the thread that's also responsible for repainting
//     the page.
//
// The two instances don't share state — the worker has no idea what
// the main thread's current position is. Every search request sends
// the position explicitly as a FEN string (see engineMoveAsync), and
// whatever move the worker decides on gets applied back on the MAIN
// thread's instance via a normal makeMove call, exactly like a human
// move would be. See cmd/wasm/main.go's searchMove doc comment for
// the Go side of this split.

// window.Dyanis.* is SYNCHRONOUS once the wasm module is loaded (Go's
// js.FuncOf runs to completion before returning to JS) — only the
// initial module load below is async. That matters for
// react-chessboard's onPieceDrop/onSquareClick, which are plain
// synchronous event handlers: makeMove needs to resolve within that
// same synchronous call, or the board would visibly snap the piece
// back before the "real" position arrives a tick later.

let readyResolve;
let readyReject;
const readyPromise = new Promise((resolve, reject) => {
  readyResolve = resolve;
  readyReject = reject;
});

/**
 * Loads wasm_exec.js's Go runtime + our dyanis.wasm binary on the
 * MAIN thread. Resolves once window.Dyanis is safe to call. Call this
 * once (e.g. in a top-level useEffect); every sync export below
 * assumes it has already resolved.
 */
export function initEngine() {
  if (typeof window.Go === "undefined") {
    readyReject(new Error("wasm_exec.js не загрузился — проверь <script> в index.html"));
    return readyPromise;
  }

  window.resolveDyanisReady = () => readyResolve(window.Dyanis);

  const go = new window.Go();
  WebAssembly.instantiateStreaming(fetch(`${import.meta.env.BASE_URL}wasm/dyanis.wasm`), go.importObject)
    .then((result) => {
      go.run(result.instance); // fire-and-forget; see comment above
    })
    .catch(readyReject);

  return readyPromise;
}

function api() {
  if (!window.Dyanis) {
    throw new Error("движок ещё не загружен — initEngine() должен завершиться раньше");
  }
  return window.Dyanis;
}

// --- Main-thread, synchronous: game bookkeeping ---------------------

export const newGame = (fen) => JSON.parse(fen ? api().newGame(fen) : api().newGame());
export const getState = () => JSON.parse(api().getState());
export const makeMove = (move) => JSON.parse(api().makeMove(move));
export const undo = () => JSON.parse(api().undo());
export const perft = (depth = 3) => JSON.parse(api().perft(depth));

// Main-thread engineMove/loadBook/bookInfo are still exposed (handy
// for quick local testing without spinning up a worker), but the
// actual app should prefer the *Worker variants below for gameplay —
// see engineMoveAsync.
export const engineMove = (depth = 3, movetimeMs = 0) => JSON.parse(api().engineMove(depth, movetimeMs));
export const loadBook = (bytes) => JSON.parse(api().loadBook(bytes));
export const clearBook = () => JSON.parse(api().clearBook());
export const bookInfo = () => JSON.parse(api().bookInfo());

// --- Worker thread, asynchronous: search -----------------------------

let worker = null;
let workerReadyPromise = null;
let nextRequestId = 1;
const pending = new Map();

function ensureWorker() {
  if (worker) return workerReadyPromise;

  worker = new Worker(`${import.meta.env.BASE_URL}engine-worker.js`);
  workerReadyPromise = new Promise((resolve, reject) => {
    worker.onmessage = (e) => {
      const { type, id, result, error } = e.data;

      if (type === "ready") {
        resolve();
        return;
      }
      if (type === "load-error") {
        reject(new Error(error));
        return;
      }
      // Response to a specific RPC call (searchMove, loadBook, ...).
      const p = pending.get(id);
      if (!p) return;
      pending.delete(id);
      if (error) p.reject(new Error(error));
      else p.resolve(result);
    };
    worker.onerror = (e) => reject(new Error(e.message || "воркер с движком не загрузился"));
  });

  return workerReadyPromise;
}

function callWorker(method, args) {
  return ensureWorker().then(
    () =>
      new Promise((resolve, reject) => {
        const id = nextRequestId++;
        pending.set(id, { resolve, reject });
        worker.postMessage({ id, method, args });
      })
  );
}

/**
 * Computes the engine's move for `fen` in a Web Worker — fully off
 * the main thread, so the page stays responsive no matter how long
 * the search takes (including an unbounded, no-time-budget search at
 * high depth). Returns a Promise resolving to
 * {ok, uci, san, fromBook} (or {ok:false, error}).
 *
 * This does NOT apply the move to the game — the caller still needs
 * to feed the returned `uci` into makeMove() on the main thread to
 * actually commit it (same as any move gets committed). Keeping
 * "decide the move" and "apply the move" as separate steps is what
 * lets the worker stay completely stateless.
 */
export function engineMoveAsync(fen, depth = 3, movetimeMs = 0) {
  return callWorker("searchMove", [fen, depth, movetimeMs]);
}

/** Loads a book into the WORKER's instance — separate from loadBook() above, which loads into the main thread's instance. engineMoveAsync only ever consults the worker's own book. */
export function loadBookInWorker(bytes) {
  return callWorker("loadBook", [bytes]);
}

export function clearBookInWorker() {
  return callWorker("clearBook", []);
}

export function bookInfoInWorker() {
  return callWorker("bookInfo", []);
}