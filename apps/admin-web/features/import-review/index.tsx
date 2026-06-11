import { DatabaseZap, FileWarning } from "lucide-react";
import { EmptyPanel, PageHeader, Panel, StatPill, ValueList } from "@/components/admin-primitives";

const localBaseline = [
  ["created_goat", 711],
  ["needs_review", 512],
  ["species_or_breed_requires_review", 504],
  ["blank_old_tag_suffix", 160],
  ["blank_gender", 4],
  ["duplicate_old_tag_same_scope", 4],
] as const;

export function ImportReviewPage() {
  return (
    <>
      <PageHeader
        eyebrow="Import Review"
        title="RFID Import Review"
        description="Import run read endpoints and review-row APIs are not exposed to admin-web yet. This page keeps the local rehearsal baseline separate from live analytics."
      />
      <div className="grid gap-5 xl:grid-cols-[0.9fr_1.1fr]">
        <Panel title="Last local rehearsal baseline" description="Static local baseline, not a live API or reviewer CSV read.">
          <div className="grid gap-3 sm:grid-cols-2">
            {localBaseline.map(([label, value]) => (
              <StatPill key={label} label={label} value={value} tone={label === "created_goat" ? "good" : "warn"} />
            ))}
          </div>
        </Panel>
        <Panel title="Live import screens">
          <div className="space-y-3">
            <Placeholder icon={<DatabaseZap className="h-5 w-5 text-[#14f1d9]" aria-hidden="true" />} title="Import run summary" />
            <Placeholder icon={<FileWarning className="h-5 w-5 text-[#facc15]" aria-hidden="true" />} title="Import rows and review buckets" />
            <Placeholder icon={<DatabaseZap className="h-5 w-5 text-[#14f1d9]" aria-hidden="true" />} title="Create import run" />
          </div>
        </Panel>
      </div>
      <div className="mt-5">
        <Panel title="Evidence path">
          <ValueList
            values={[
              ["Reviewer CSVs", "Generated locally by CLI and not read by this UI"],
              ["Canonical records", "Created only by backend import/apply workers"],
              ["Corrections", "Correction list/admin screens are not implemented in this read-only slice"],
              ["Goat writes", "Create/update screens are not implemented in this read-only slice"],
            ]}
          />
        </Panel>
      </div>
    </>
  );
}

function Placeholder({ icon, title }: { icon: React.ReactNode; title: string }) {
  return (
    <div className="rounded-md border border-dashed border-[#334155] bg-[#10141b] p-4">
      <div className="flex items-center gap-2 font-semibold text-white">
        {icon}
        {title}
      </div>
      <div className="mt-2">
        <EmptyPanel message="No live read endpoint is wired for this screen yet." />
      </div>
    </div>
  );
}
