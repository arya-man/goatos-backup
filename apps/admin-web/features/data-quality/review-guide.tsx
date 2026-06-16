import Link from "next/link";
import { ArrowLeft, CircleHelp, Database, FileSearch, ListChecks, ShieldAlert } from "lucide-react";
import { Mono, PageHeader, Panel } from "@/components/admin-primitives";
import { bulkDecisions, reviewGroupLabel } from "./review-groups";

const conflictGroups = [
  {
    title: "Legacy self-conflict",
    examples: ["Legacy gender is female|male for the same matched identifier.", "Legacy breed is Beetal|Mixed for the same matched identifier."],
    lookup: "Search legacy source rows by the identifier shown in the workbench: RFID, old tag, farm, and date. Find whether the rows are one goat with bad entry, a reused tag, or two goats mixed together.",
    action: "Do not auto-close. If the source rows are mixed, mark the identifier disputed or request field check. Reject only when the reviewer can prove the conflict is a harmless duplicate alert and writes the evidence reference.",
  },
  {
    title: "Legacy vs Mesha attribute mismatch",
    examples: ["Mesha passport says female, clean legacy source rows say male.", "Mesha passport says one breed, clean legacy source rows say another breed."],
    lookup: "Open the legacy rows around the latest event date, then compare gender, breed, farm, shed, old tag, RFID, and any original Sheet row. Check whether the latest clean row is actually for the same physical goat.",
    action: "If legacy is right but the passport needs a field change, create or approve a correction request with the source row evidence. If identity is wrong, dispute the identifier or request field check. Do not use blind field overwrite.",
  },
  {
    title: "RFID vs old-tag lifecycle mismatch",
    examples: ["RFID history says dead, old-tag history later says alive.", "RFID says sold, old tag has later non-purchase activity."],
    lookup: "Use RFID as the stronger identity signal, but inspect old-tag rows for tag reuse, manual old-tag edits, or rows attached to the wrong animal. Check sale/death dates and any later shifting/feed/health activity.",
    action: "Keep it open until a reviewer explains the old-tag side. If the old tag was reused, dispute or retire the old-tag identifier. If the RFID lifecycle is wrong, request field check or correction with evidence.",
  },
];

export function DataQualityReviewGuidePage() {
  return (
    <>
      <PageHeader
        eyebrow="Data Quality"
        title="Legacy Review Guide"
        description="How legacy-data reviewers should investigate source-row disagreements before recording a Mesha passport decision."
        actions={
          <Link
            href="/data-quality"
            className="inline-flex h-10 items-center gap-2 rounded-md border border-[#334155] px-3 text-sm font-semibold text-[#c7d1dc] hover:border-[#14f1d9]/70 hover:text-white"
          >
            <ArrowLeft className="h-4 w-4" aria-hidden="true" />
            Back to queues
          </Link>
        }
      />

      <div className="grid gap-5">
        <Panel title="Golden rule" description="Legacy data is evidence, not an automatic overwrite. Mesha keeps the passport unchanged until a human decision is backed by source evidence.">
          <div className="grid gap-3 md:grid-cols-3">
            <GuidePoint icon={<ShieldAlert className="h-5 w-5 text-[#facc15]" aria-hidden="true" />} title="Do not auto-close self-conflicts">
              If legacy source rows say both values for the same matched identifier, that is a data-quality problem to review, not a completed decision.
            </GuidePoint>
            <GuidePoint icon={<Database className="h-5 w-5 text-[#14f1d9]" aria-hidden="true" />} title="Look up the source rows">
              Use the identifier, farm, event date, gender, breed, shed, RFID, and old tag shown in the workbench to find the matching legacy rows.
            </GuidePoint>
            <GuidePoint icon={<FileSearch className="h-5 w-5 text-[#7dd3fc]" aria-hidden="true" />} title="Record the evidence">
              Every decision needs an evidence type, evidence ID, source system, and a reason that another reviewer can follow later.
            </GuidePoint>
          </div>
        </Panel>

        <Panel title="What reviewers should check in legacy" description="Use these fields before choosing a decision in Mesha.">
          <div className="grid gap-3 lg:grid-cols-2">
            <LookupItem label="Legacy source fields" values={["goat identifier", "farm goat number", "farm", "event type", "event date", "gender", "breed", "current shed", "source shed", "destination shed"]} />
            <LookupItem label="Mesha workbench fields" values={["display ID", "RFID", "old tag", "scope and farm", "breed", "sex", "lifecycle", "location", "legacy reason", "legacy value"]} />
            <LookupItem label="Identity clues" values={["same RFID across rows", "old tag reused across farms", "row date order", "sale/death before later activity", "same shed/partition path"]} />
            <LookupItem label="Decision evidence" values={["source_record ID", "conflict ID", "identifier key", "import run", "field check note", "legacy Sheet row reference"]} />
          </div>
        </Panel>

        <Panel title="How to resolve each group" description="The queue groups are different. Treat them differently.">
          <div className="grid gap-3">
            {conflictGroups.map((group) => (
              <div key={group.title} className="border-l border-[#334155] py-1 pl-4">
                <div className="flex items-start gap-3">
                  <CircleHelp className="mt-0.5 h-5 w-5 text-[#14f1d9]" aria-hidden="true" />
                  <div>
                    <h2 className="text-sm font-bold text-white">{group.title}</h2>
                    <ul className="mt-2 list-disc space-y-1 pl-5 text-sm leading-6 text-[#c7d1dc]">
                      {group.examples.map((example) => (
                        <li key={example}>{example}</li>
                      ))}
                    </ul>
                    <p className="mt-3 text-sm leading-6 text-[#93a4b8]">{group.lookup}</p>
                    <p className="mt-2 text-sm leading-6 text-[#c7d1dc]">{group.action}</p>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </Panel>

        <Panel
          title="Bulk review actions"
          description="On the Data Quality queue you can filter by review group, tick the conflicts you have reviewed, and apply one decision to the whole selection. Every bulk decision records the reason on each conflict and is audited."
        >
          <div className="grid gap-3">
            {bulkDecisions.map((decision) => (
              <div key={decision.type} className="border-l border-[#334155] py-1 pl-4">
                <div className="flex items-start gap-3">
                  <ListChecks className="mt-0.5 h-5 w-5 text-[#14f1d9]" aria-hidden="true" />
                  <div>
                    <h2 className="text-sm font-bold text-white">{decision.label}</h2>
                    <p className="mt-2 text-sm leading-6 text-[#c7d1dc]">{decision.intent}</p>
                    <p className="mt-2 text-xs text-[#93a4b8]">
                      Applies to: {decision.appliesTo.map((group) => reviewGroupLabel(group)).join(", ")}.
                    </p>
                  </div>
                </div>
              </div>
            ))}
            <p className="border-l border-[#334155] py-1 pl-4 text-sm leading-6 text-[#93a4b8]">
              A bulk batch is applied together. If any selected conflict has changed since the page loaded, none are changed - reload and retry. Bulk
              actions only cover the rows visible on the current page; there is no select-all-across-pages and no spreadsheet import or export. After a bulk
              change, identity counters may show as needing a rebuild until the next counter run.
            </p>
          </div>
        </Panel>
      </div>
    </>
  );
}

function GuidePoint({ icon, title, children }: { icon: React.ReactNode; title: string; children: React.ReactNode }) {
  return (
    <div className="border-l border-[#334155] py-1 pl-4">
      <div className="flex items-start gap-3">
        {icon}
        <div>
          <h2 className="text-sm font-bold text-white">{title}</h2>
          <p className="mt-2 text-sm leading-6 text-[#93a4b8]">{children}</p>
        </div>
      </div>
    </div>
  );
}

function LookupItem({ label, values }: { label: string; values: string[] }) {
  return (
    <div className="border-l border-[#334155] py-1 pl-4">
      <div className="text-xs font-semibold uppercase text-[#14f1d9]">{label}</div>
      <div className="mt-3 flex flex-wrap gap-2">
        {values.map((value) => (
          <Mono key={value}>{value}</Mono>
        ))}
      </div>
    </div>
  );
}
