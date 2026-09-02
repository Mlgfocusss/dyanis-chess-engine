export default function MoveLog({ log }) {
  return (
    <div className="move-log">
      <h2>Партия</h2>
      <pre className="move-log__text">{log || "—"}</pre>
    </div>
  );
}