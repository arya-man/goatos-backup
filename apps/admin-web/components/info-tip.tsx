/**
 * InfoTip is the small "i" beside a title or column heading. Hover or keyboard focus reveals the
 * note; it is CSS-only (`.tipwrap` in mesha-theme.css), so it works in a server component and the
 * note stays in the DOM for screen readers.
 */
export function InfoTip({ label, text, end = false }: { label: string; text: string; end?: boolean }) {
  return (
    <span className="tipwrap">
      <button type="button" className="ihelp" aria-label={label}>
        i
      </button>
      <span className={end ? "tip tip-end" : "tip"} role="tooltip">
        {text}
      </span>
    </span>
  );
}
