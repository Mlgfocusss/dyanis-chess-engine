import { useMemo, useState } from "react";
import { Chessboard } from "react-chessboard";
import { useElementWidth } from "../hooks/useElementWidth.js";

// state: stateResponse from dyanisEngine.getState()/makeMove()/etc.
// onPlayerMove(uci) -> moveResponse; called for both drag-drop and
// click-click attempts once a legal from/to pair is picked.
export default function Board({ state, orientation, disabled, lastMove, onPlayerMove }) {
  const [selectedSquare, setSelectedSquare] = useState(null);
  const [containerRef, width] = useElementWidth();

  const movesBySquare = useMemo(() => {
    const map = new Map();
    for (const m of state.legalMoves) {
      const from = m.uci.slice(0, 2);
      if (!map.has(from)) map.set(from, []);
      map.get(from).push(m);
    }
    return map;
  }, [state.legalMoves]);

  const legalTargetSquares = useMemo(() => {
    if (!selectedSquare) return new Set();
    return new Set((movesBySquare.get(selectedSquare) || []).map((m) => m.uci.slice(2, 4)));
  }, [selectedSquare, movesBySquare]);

  // Plays from->to if it's a legal move. When more than one candidate
  // matches (only happens for promotion), defaults to queen — same
  // convention the engine's own ParseSAN uses server-side for an
  // unspecified promotion, so behavior matches the CLI. A proper
  // promotion-piece picker is a reasonable follow-up, not blocking.
  function attemptMove(from, to) {
    const candidates = (movesBySquare.get(from) || []).filter((m) => m.uci.slice(2, 4) === to);
    if (candidates.length === 0) return false;
    const choice = candidates.find((m) => m.uci.endsWith("q")) || candidates[0];
    const result = onPlayerMove(choice.uci);
    return result?.ok ?? false;
  }

  function handleSquareClick(square) {
    if (disabled) return;
    if (selectedSquare === square) {
      setSelectedSquare(null);
      return;
    }
    if (selectedSquare && legalTargetSquares.has(square)) {
      const moved = attemptMove(selectedSquare, square);
      setSelectedSquare(null);
      if (moved) return;
    }
    setSelectedSquare(movesBySquare.has(square) ? square : null);
  }

  function handlePieceDrop(sourceSquare, targetSquare) {
    if (disabled) return false;
    const moved = attemptMove(sourceSquare, targetSquare);
    setSelectedSquare(null);
    return moved;
  }

  const squareStyles = useMemo(() => {
    const styles = {};
    if (lastMove) {
      styles[lastMove.from] = { backgroundColor: "rgba(205, 210, 106, 0.65)" };
      styles[lastMove.to] = { backgroundColor: "rgba(205, 210, 106, 0.65)" };
    }
    if (selectedSquare) {
      styles[selectedSquare] = { ...(styles[selectedSquare] || {}), backgroundColor: "rgba(255, 230, 90, 0.55)" };
    }
    for (const sq of legalTargetSquares) {
      styles[sq] = { ...(styles[sq] || {}), boxShadow: "inset 0 0 0 4px rgba(20, 85, 30, 0.55)" };
    }
    return styles;
  }, [lastMove, selectedSquare, legalTargetSquares]);

  return (
    <div ref={containerRef} className="board-wrap">
      <Chessboard
        id="DyanisBoard"
        position={state.fen}
        boardWidth={width}
        boardOrientation={orientation}
        onPieceDrop={handlePieceDrop}
        onSquareClick={handleSquareClick}
        isDraggablePiece={({ sourceSquare }) => !disabled && movesBySquare.has(sourceSquare)}
        customSquareStyles={squareStyles}
        customDarkSquareStyle={{ backgroundColor: "#7c6a58" }}
        customLightSquareStyle={{ backgroundColor: "#eee0c9" }}
        customBoardStyle={{ borderRadius: "6px", boxShadow: "0 10px 30px rgba(0,0,0,0.35)" }}
        animationDuration={200}
        autoPromoteToQueen
      />
    </div>
  );
}