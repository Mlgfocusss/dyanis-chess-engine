// Runs entirely inside a Web Worker: its own thread, its own copy of
// the wasm module, its own memory. The main thread's game state
// (position, history, move log) lives in a COMPLETELY SEPARATE
// instance over there — this worker only ever answers "given this
// FEN, what's the best move?" (see cmd/wasm/main.go's searchMove).
// That's what lets an unbounded-depth or no-time-budget search run in
// here without ever freezing the page: the main thread is free to
// keep repainting the whole time, because it was never the one doing
// the work.
//
// Plain classic worker (not `type: module`), because wasm_exec.js
// isn't an ES module — importScripts is the classic-worker equivalent
// of the <script src="..."> tag index.html uses on the main thread.
importScripts("wasm/wasm_exec.js");

const go = new self.Go();
let ready = false;

// See cmd/wasm/main.go's notifyReady: go.run()'s own promise never
// resolves (main() blocks forever on select{} to stay alive for JS
// callbacks), so this explicit hook is how Go tells us window/self.
// Dyanis is actually safe to call now.
self.resolveDyanisReady = () => {
  ready = true;
  postMessage({ type: "ready" });
};

fetch("wasm/dyanis.wasm")
  .then((resp) => WebAssembly.instantiateStreaming(resp, go.importObject))
  .then((result) => {
    go.run(result.instance); // fire-and-forget; see cmd/wasm/main.go's own comment on this
  })
  .catch((err) => {
    postMessage({ type: "load-error", error: String(err) });
  });

// RPC: { id, method, args } in -> { id, result } or { id, error } out.
// Generic on purpose — every Dyanis.* function already returns a JSON
// string (see cmd/wasm/main.go), so this doesn't need per-method
// special-casing, just JSON.parse the return value uniformly.
self.onmessage = (e) => {
  const { id, method, args } = e.data;

  if (!ready) {
    postMessage({ id, error: "движок в воркере ещё не загрузился" });
    return;
  }

  const fn = self.Dyanis[method];
  if (typeof fn !== "function") {
    postMessage({ id, error: `неизвестный метод Dyanis: ${method}` });
    return;
  }

  try {
    const result = JSON.parse(fn(...args));
    postMessage({ id, result });
  } catch (err) {
    postMessage({ id, error: String(err) });
  }
};