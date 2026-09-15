import { useWorkspace } from './bridge';

// This root is a sibling of #live-session: hiding an unrelated owner must not
// hide the opening status or collapse its reserved geometry. No transport here.
export function Opening() {
  const { opening } = useWorkspace();
  if (!opening.visible) return null;
  return (
    <div
      id="workspace-opening"
      className="empty-state compact workspace-opening"
      role="status"
      aria-live="polite"
      style={{ minHeight: `min(${opening.minHeight}px, 100dvh)` }}
    >
      <p>Opening selected conversation…</p>
    </div>
  );
}
