import Link from "next/link";
import { PageHeader } from "@/components/admin-primitives";
import { ScheduleBuilder } from "./schedule-builder";

export function ConfigSchedulePage() {
  return (
    <div className="min-w-0">
      <PageHeader
        eyebrow="PHC · Vaccination"
        title="Config — Schedule Builder"
        description="What should happen. Author + publish the medical schedule; obligations, SOP tasks and adherence all flow from PUBLISHED, source-backed rules. Drafts never generate live work."
        actions={
          <Link
            href="/vaccination"
            className="inline-flex min-h-[40px] items-center rounded-md border border-[#334155] px-3 py-2 text-sm text-[#c7d1dc] hover:border-[#14f1d9]/40"
          >
            ← Action Center
          </Link>
        }
      />
      <div className="mb-4 flex items-start gap-2 rounded-xl border border-[#a16207] bg-[#1f1a07] px-4 py-3 text-sm text-[#facc15]">
        <span aria-hidden>⚠</span>
        <div>
          Real business/medical config — <b>not public</b>. Only <b>CEO/COO publish</b>; field / verifier / park never
          see raw config (they get generated obligations + SOP tasks only). No vaccine values are invented — a draft
          publishes only when source-backed (vaccinations_db / phc / vet, reviewed, approved).
        </div>
      </div>
      <ScheduleBuilder />
    </div>
  );
}
