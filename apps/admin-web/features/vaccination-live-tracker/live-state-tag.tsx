import { Tag, type Tone } from "@/components/ui-primitives";
import { optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// The mock's row status pill for an operator or shed that is happening RIGHT NOW is
// `<span class="tag t-live"><i></i>active now</span>` (mock lines 237, 280). The `<i>` is the
// pulsing dot, and it is the ONLY thing distinguishing that pill from the static red `.t-dng` pill
// used for `idle` and `not_started` — the two share a background by design in the mock as well.
//
// The shared `Tag` primitive renders no `<i>` child and its `Tone` union does not contain "live", so
// the previous call sites cast the backend tone with `as Tone`: tsc passed, `.t-live` painted the
// same red wash as `.t-dng`, no dot was ever emitted, and "active now", "idle" and "not started"
// became three visually indistinguishable pills. This component is the one place that knows the
// backend can return the page-local "live" tone, so no call site has to lie to the type checker.
const DESIGN_SYSTEM_TONES: ReadonlySet<string> = new Set(["ok", "warn", "dng", "info", "mut", "pur", "teal"]);

export function LiveStateTag({
  pageContract,
  group,
  stateKey,
  title,
  children,
}: {
  pageContract: AdminUiPageContract;
  group: string;
  stateKey: string;
  title?: string;
  children: React.ReactNode;
}) {
  const tone = optionTone(pageContract, group, stateKey);
  if (tone === "live") {
    return (
      <span className="tag t-live" title={title}>
        <i />
        {children}
      </span>
    );
  }
  return (
    <Tag tone={(DESIGN_SYSTEM_TONES.has(tone) ? tone : "mut") as Tone} title={title}>
      {children}
    </Tag>
  );
}
