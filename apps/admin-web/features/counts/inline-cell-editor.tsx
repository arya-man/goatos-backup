"use client";

// The shared inline editor behind every editable cell on Counts Breakdown.
//
// It owns the INTERACTION and nothing else: double-click to open, type to narrow, pick, confirm,
// apply. What each cell actually writes -- a pen's cohort tag, a row's breed, a row's sex -- is
// supplied by the caller as two functions, because those writes have genuinely different scopes and
// must stay visibly different in the code that calls them.
//
// Double-click rather than single: every cell in this table is a census figure an operator reads,
// and a single click that opened an editor would fire constantly while scanning the page. Enter and
// Space open it too, so the control is reachable without a mouse.
//
// CHECK-THEN-APPLY is not optional here. These writes have no approval step and no proof behind
// them, so the preview IS the safety mechanism: picking a value shows how many animals it would
// move, and only then does the button that applies it appear.
//
// Every visible string comes from the page contract, including the default reason that lands in the
// audit row. This component composes no copy and decides no authority.
import { useEffect, useLayoutEffect, useMemo, useRef, useState, useTransition } from "react";
import { createPortal } from "react-dom";
import { useRouter } from "next/navigation";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// InlineChoice is one selectable value. `label` is what the cell will show once applied -- the
// stored value, not a prettier synonym -- and `description`/`hint` are secondary context.
export type InlineChoice = {
  value: string;
  label: string;
  description?: string;
  hint?: string;
};

// InlinePreview is what the caller's preview call answered. `subject` names the thing being
// changed (the backend's own composed location string), `count` is how many animals move, and
// `consequence` is an optional extra line -- the kid/adult band, for instance.
export type InlinePreview = {
  subject: string;
  count: number;
  consequence?: string;
};

// Named aliases rather than inline `=> Promise<...>` signatures: the contract-literal guard reads
// an arrow type followed by a generic as JSX text and flags it. The indirection costs nothing and
// keeps the guard able to see real violations instead of being switched off here.
type PreviewOutcome = Promise<InlinePreview | { error: string }>;
type ApplyOutcome = Promise<{ error?: string }>;

type Phase =
  | { kind: "closed" }
  | { kind: "picking" }
  | { kind: "checking"; value: string }
  | { kind: "confirming"; value: string; preview: InlinePreview }
  | { kind: "failed"; message: string };

export function InlineCellEditor({
  pageContract,
  current,
  emptyLabel,
  choices,
  enabled,
  disabledReason,
  onPreview,
  onApply,
  renderCurrent,
}: {
  pageContract: AdminUiPageContract;
  current: string;
  emptyLabel: string;
  choices: InlineChoice[];
  enabled: boolean;
  disabledReason: string;
  onPreview: (value: string) => PreviewOutcome;
  onApply: (value: string, reason: string, idempotencyKey: string) => ApplyOutcome;
  renderCurrent?: (current: string) => React.ReactNode;
}) {
  const router = useRouter();
  const [phase, setPhase] = useState<Phase>({ kind: "closed" });
  const [filter, setFilter] = useState("");
  const [reason, setReason] = useState("");
  const [pending, startTransition] = useTransition();
  const rootRef = useRef<HTMLDivElement>(null);
  const popRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // The popup renders through a portal with FIXED coordinates, because the cell sits inside the
  // table's horizontal-scroll container: an absolutely positioned child is clipped at that
  // container's edge, so a short table cut the list off entirely. Measured in a layout effect
  // (before paint, so it never flashes at 0,0) and re-placed on scroll/resize; it flips above the
  // cell when the space below the anchor cannot hold it.
  const [popStyle, setPopStyle] = useState<React.CSSProperties | null>(null);

  // One key per INTENT, minted when a preview is accepted and held across retries, so a double
  // click or a retried network failure replays the first write instead of writing twice.
  const [commitKey, setCommitKey] = useState("");

  const open = phase.kind !== "closed";
  const toggleOpen = () => (open ? close() : setPhase({ kind: "picking" }));

  // Outside click and Escape close it, matching every other same-page overlay in the app. Local
  // state only: this never navigates, so the row behind it is not re-fetched on open or close.
  useEffect(() => {
    if (!open) return undefined;
    const onPointerDown = (event: MouseEvent) => {
      const target = event.target as Node;
      if (rootRef.current?.contains(target)) return;
      if (popRef.current?.contains(target)) return;
      close();
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  });

  useEffect(() => {
    if (phase.kind === "picking") inputRef.current?.focus();
  }, [phase.kind]);
  function close() {
    setPhase({ kind: "closed" });
    setFilter("");
    setCommitKey("");
  }

  // Substring match on the stored value AND its description, because an operator who knows the
  // vocabulary types "f2" as readily as "fattening".
  const matches = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    if (!needle) return choices;
    return choices.filter(
      (choice) =>
        choice.value.toLowerCase().includes(needle) ||
        choice.label.toLowerCase().includes(needle) ||
        (choice.description ?? "").toLowerCase().includes(needle),
    );
  }, [filter, choices]);

  // matches.length is a dependency because narrowing the list changes the popup's height, which
  // can change whether it still fits below the anchor.
  // A stale popStyle from a previous open never paints: this layout effect re-runs before the
  // browser paints the reopened popup and re-places it from the fresh anchor rect.
  useLayoutEffect(() => {
    if (!open) return undefined;
    const place = () => {
      const anchor = rootRef.current?.getBoundingClientRect();
      const pop = popRef.current;
      if (!anchor || !pop) return;
      const gap = 6;
      const margin = 8;
      const width = pop.offsetWidth || 250;
      const height = pop.offsetHeight;
      const left = Math.max(margin, Math.min(anchor.left, window.innerWidth - width - margin));
      let top = anchor.bottom + gap;
      if (top + height > window.innerHeight - margin && anchor.top - gap - height >= margin) {
        top = anchor.top - gap - height;
      }
      setPopStyle({ position: "fixed", top, left });
    };
    place();
    // Capture-phase scroll so the table's own horizontal-scroll container repositions the popup
    // too, not just the document scroll.
    window.addEventListener("scroll", place, true);
    window.addEventListener("resize", place);
    return () => {
      window.removeEventListener("scroll", place, true);
      window.removeEventListener("resize", place);
    };
  }, [open, phase.kind, matches.length]);

  function pick(value: string) {
    setPhase({ kind: "checking", value });
    startTransition(async () => {
      const preview = await onPreview(value);
      if ("error" in preview) {
        setPhase({ kind: "failed", message: preview.error || copy(pageContract, "action.retag.failed") });
        return;
      }
      setReason(copy(pageContract, "action.retag.default_reason"));
      setCommitKey(crypto.randomUUID());
      setPhase({ kind: "confirming", value, preview });
    });
  }

  function apply(value: string) {
    startTransition(async () => {
      const result = await onApply(value, reason.trim(), commitKey);
      if (result.error) {
        setPhase({ kind: "failed", message: result.error });
        return;
      }
      close();
      router.refresh();
    });
  }

  if (!enabled) {
    // Disabled-with-reason, never hidden: the backend decides authority and says why, and a control
    // that comes and goes reads as a broken screen rather than a withheld one.
    return (
      <span title={disabledReason} aria-disabled="true">
        {current ? (renderCurrent?.(current) ?? current) : <span className="muted small">{emptyLabel}</span>}
      </span>
    );
  }

  return (
    <div ref={rootRef} className="tagedit">
      <button
        type="button"
        className="tagedit-value"
        onClick={toggleOpen}
        onKeyDown={(event) => {
          if (event.key !== "Enter" && event.key !== " ") return;
          event.preventDefault();
          toggleOpen();
        }}
        aria-expanded={open}
        aria-haspopup="dialog"
        title={copy(pageContract, "action.retag.hint")}
      >
        {current ? (renderCurrent?.(current) ?? current) : <span className="muted small">{emptyLabel}</span>}
      </button>

      {open
        ? createPortal(
            <div
              ref={popRef}
              className="tagedit-pop"
              // Hidden until the layout effect has measured and placed it, so the first paint never
              // shows the popup at the viewport origin.
              style={popStyle ?? { position: "fixed", top: 0, left: 0, visibility: "hidden" }}
              role="dialog"
              aria-label={copy(pageContract, "action.retag.hint")}
            >
          {phase.kind === "picking" || phase.kind === "checking" ? (
            <>
              <input
                ref={inputRef}
                type="text"
                value={filter}
                onChange={(event) => setFilter(event.target.value)}
                placeholder={copy(pageContract, "action.retag.search_placeholder")}
                aria-label={copy(pageContract, "action.retag.search_placeholder")}
              />
              <div className="tagedit-list">
                {matches.length === 0 ? (
                  <div className="muted small tagedit-empty">{copy(pageContract, "action.retag.no_matches")}</div>
                ) : (
                  matches.map((choice) => (
                    <button
                      key={choice.value}
                      type="button"
                      className="tagedit-option"
                      disabled={pending}
                      onClick={() => pick(choice.value)}
                    >
                      <span className="tagedit-option-name">
                        <span>{choice.label}</span>
                        {choice.description ? <span className="muted small">{choice.description}</span> : null}
                      </span>
                      {choice.hint ? <span className="muted small">{choice.hint}</span> : null}
                    </button>
                  ))
                )}
              </div>
              {phase.kind === "checking" ? (
                <div className="muted small tagedit-foot">{copy(pageContract, "action.retag.checking")}</div>
              ) : null}
            </>
          ) : null}

          {phase.kind === "confirming" ? (
            <div className="tagedit-confirm">
              {/* Subject and count come from the PREVIEW, never from the row: the two can differ,
                  and the operator must confirm what the write will actually do. */}
              <div className="tagedit-confirm-title">
                <b>{phase.value}</b> · {phase.preview.subject}
              </div>
              <div className="muted small">
                {phase.preview.count === 0
                  ? copy(pageContract, "action.retag.empty_scope")
                  : `${phase.preview.count} ${
                      phase.preview.count === 1
                        ? copy(pageContract, "action.retag.animal_noun")
                        : copy(pageContract, "action.retag.animals_noun")
                    }`}
              </div>
              {phase.preview.consequence ? (
                <div className="small tagedit-band">{phase.preview.consequence}</div>
              ) : null}
              <label className="tagedit-reason">
                <span className="muted small">{copy(pageContract, "action.retag.reason_label")}</span>
                <input
                  type="text"
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                  maxLength={500}
                  aria-label={copy(pageContract, "action.retag.reason_label")}
                />
              </label>
              <div className="tagedit-actions">
                <button type="button" className="btn sm" onClick={close} disabled={pending}>
                  {copy(pageContract, "action.retag.cancel")}
                </button>
                <button
                  type="button"
                  className="btn sm primary"
                  onClick={() => apply(phase.value)}
                  // The backend requires 3..500 characters, so the button states that rule rather
                  // than letting the operator discover it as a server error.
                  disabled={pending || reason.trim().length < 3}
                >
                  {pending ? copy(pageContract, "action.retag.applying") : copy(pageContract, "action.retag.apply")}
                </button>
              </div>
            </div>
          ) : null}

          {phase.kind === "failed" ? (
            <div className="tagedit-confirm">
              <div className="small">{phase.message}</div>
              <div className="tagedit-actions">
                <button type="button" className="btn sm" onClick={() => setPhase({ kind: "picking" })}>
                  {copy(pageContract, "action.retag.cancel")}
                </button>
              </div>
            </div>
          ) : null}
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}
