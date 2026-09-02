import { useCallback, useEffect, useState } from "react";
import Board from "./components/Board.jsx";
import MoveLog from "./components/MoveLog.jsx";
import * as engine from "./engine/dyanisEngine.js";
import "./App.css";

const DEFAULT_DEPTH = 4;
const DEFAULT_MOVETIME_MS = 800;

export default function App() {
  const [state, setState] = useState(null);
  const [error, setError] = useState(null);
  const [thinking, setThinking] = useState(false);
  const [playerSide, setPlayerSide] = useState("white");
  const [bookStatus, setBookStatus] = useState({ loaded: false, entries: 0 });
  const [lastMove, setLastMove] = useState(null);
  const [engineNote, setEngineNote] = useState("");

  // Search strength controls. depth is always the max-depth ceiling;
  // when useMovetime is on, the engine uses iterative deepening with
  // movetimeMs as a time budget instead of always searching to a
  // fixed depth — same relationship -depth/-movetime have in the
  // CLI's -play (see cmd/cli/main.go). Turning useMovetime off means
  // "search exactly to `depth`, however long that takes" — safe to do
  // now even at high depth, since the search runs in a Web Worker and
  // can no longer freeze the page (see engine/dyanisEngine.js).
  const [depth, setDepth] = useState(DEFAULT_DEPTH);
  const [useMovetime, setUseMovetime] = useState(true);
  const [movetimeMs, setMovetimeMs] = useState(DEFAULT_MOVETIME_MS);

  const refreshBook = useCallback(() => {
    engine
      .bookInfoInWorker()
      .then(setBookStatus)
      .catch(() => {
        // воркер ещё не готов — не критично, просто пропускаем
      });
  }, []);

  useEffect(() => {
    let cancelled = false;
    engine
      .initEngine()
      .then(() => {
        if (cancelled) return;
        setState(engine.getState());
        refreshBook();
      })
      .catch((e) => setError(String(e)));
    return () => {
      cancelled = true;
    };
  }, [refreshBook]);

  const triggerEngineIfNeeded = useCallback(
    async (freshState, side = playerSide) => {
      if (!freshState || freshState.status !== "ongoing") return;
      const engineToMove =
        (freshState.sideToMove === "white" && side === "black") ||
        (freshState.sideToMove === "black" && side === "white");
      if (!engineToMove) return;

      setThinking(true);
      try {
        const budget = useMovetime ? movetimeMs : 0;
        // Runs in a Web Worker — the page stays fully responsive no
        // matter how long this takes. No "let React paint first" hack
        // needed here (unlike a synchronous main-thread call would
        // require): awaiting a worker call never blocks anything.
        const decision = await engine.engineMoveAsync(freshState.fen, depth, budget);
        if (!decision.ok) {
          setError(decision.error);
          return;
        }
        // The worker only DECIDED the move; committing it to the
        // actual tracked game state always goes through the main
        // thread's own synchronous makeMove, same as a human move.
        const result = engine.makeMove(decision.uci);
        if (result.ok) {
          setState(result.state);
          setLastMove({ from: decision.uci.slice(0, 2), to: decision.uci.slice(2, 4) });
          setEngineNote(decision.fromBook ? `движок сыграл ${decision.san} (из книги)` : `движок сыграл ${decision.san}`);
          setError(null);
        } else {
          setError(result.error);
        }
      } catch (e) {
        setError(String(e));
      } finally {
        setThinking(false);
      }
    },
    [playerSide, depth, useMovetime, movetimeMs]
  );

  function handlePlayerMove(uci) {
    const result = engine.makeMove(uci);
    if (result.ok) {
      setState(result.state);
      setLastMove({ from: uci.slice(0, 2), to: uci.slice(2, 4) });
      setEngineNote("");
      setError(null);
      triggerEngineIfNeeded(result.state); // fire-and-forget
    } else {
      setError(result.error);
    }
    return result;
  }

  function handleNewGame() {
    const s = engine.newGame();
    setState(s);
    setLastMove(null);
    setEngineNote("");
    setError(null);
    triggerEngineIfNeeded(s);
  }

  function handleUndo() {
    const result = engine.undo();
    if (result.ok) {
      setState(result.state);
      setLastMove(null);
      setEngineNote("");
    }
  }

  function handleSwitchSide(side) {
    setPlayerSide(side);
    if (state) triggerEngineIfNeeded(state, side);
  }

  async function handleLoadBook() {
    try {
      const res = await fetch(`${import.meta.env.BASE_URL}assets/gm2001.bin`);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const buf = await res.arrayBuffer();
      await engine.loadBookInWorker(new Uint8Array(buf));
      refreshBook();
    } catch (e) {
      setError(`не удалось загрузить книгу: ${e}`);
    }
  }

  async function handleClearBook() {
    await engine.clearBookInWorker();
    refreshBook();
  }

  if (!state) {
    return (
      <div className="app app--loading">
        {error ? <p className="banner banner--error">{error}</p> : <p>Загружаю движок…</p>}
      </div>
    );
  }

  return (
    <div className="app">
      <header className="app__header">
        <h1>Dyanis</h1>
        <p className="app__tagline">Собственный шахматный движок на Go, скомпилированный в WASM</p>
      </header>

      {error && <div className="banner banner--error">{error}</div>}

      <div className="layout">
        <Board
          state={state}
          orientation={playerSide}
          disabled={thinking || state.status !== "ongoing"}
          lastMove={lastMove}
          onPlayerMove={handlePlayerMove}
        />

        <aside className="sidebar">
          <div className="status">
            <p className="status__turn">
              Ход: <strong>{state.sideToMove === "white" ? "белые" : "чёрные"}</strong>
            </p>
            {state.inCheck && state.status === "ongoing" && <p className="status__flag status__flag--warn">Шах</p>}
            {state.status === "checkmate" && <p className="status__flag status__flag--warn">Мат</p>}
            {state.status === "stalemate" && <p className="status__flag status__flag--warn">Пат — ничья</p>}
            {thinking && <p className="status__flag status__flag--thinking">движок думает… (страница не зависает)</p>}
            {engineNote && <p className="status__note">{engineNote}</p>}
          </div>

          <div className="controls">
            <button onClick={handleNewGame}>Новая партия</button>
            <button onClick={handleUndo} disabled={thinking}>
              Отменить ход
            </button>

            <div className="side-toggle">
              <label>
                <input
                  type="radio"
                  name="side"
                  checked={playerSide === "white"}
                  onChange={() => handleSwitchSide("white")}
                />
                Играю за белых
              </label>
              <label>
                <input
                  type="radio"
                  name="side"
                  checked={playerSide === "black"}
                  onChange={() => handleSwitchSide("black")}
                />
                Играю за чёрных
              </label>
            </div>

            <div className="depth-controls">
              <label className="depth-controls__row">
                Глубина (потолок):
                <select value={depth} onChange={(e) => setDepth(Number(e.target.value))}>
                  {[1, 2, 3, 4, 5, 6, 7, 8].map((d) => (
                    <option key={d} value={d}>
                      {d}
                    </option>
                  ))}
                </select>
              </label>

              <label className="depth-controls__row">
                <input type="checkbox" checked={useMovetime} onChange={(e) => setUseMovetime(e.target.checked)} />
                Ограничить по времени
              </label>

              {useMovetime && (
                <label className="depth-controls__row">
                  Бюджет, мс:
                  <input
                    type="number"
                    min={100}
                    step={100}
                    value={movetimeMs}
                    onChange={(e) => setMovetimeMs(Math.max(100, Number(e.target.value) || 100))}
                  />
                </label>
              )}

              <p className="depth-controls__hint">
                {useMovetime
                  ? `итеративное углубление до глубины ${depth}, но не дольше ${movetimeMs} мс`
                  : `всегда ровно глубина ${depth} — может занять много времени, но страницу это больше не подвесит`}
              </p>
            </div>

            <div className="book-controls">
              <button onClick={handleLoadBook} disabled={bookStatus.loaded}>
                Загрузить книгу
              </button>
              <button onClick={handleClearBook} disabled={!bookStatus.loaded}>
                Отключить книгу
              </button>
              <span className="book-controls__status">
                {bookStatus.loaded ? `книга: ${bookStatus.entries} записей` : "книга: не загружена"}
              </span>
            </div>
          </div>

          <MoveLog log={state.log} />
        </aside>
      </div>
    </div>
  );
}