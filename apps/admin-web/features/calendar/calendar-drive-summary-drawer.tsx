import Link from "next/link";
import { CalendarDays, Layers3, MapPinned, Syringe, X } from "lucide-react";
import { Tag } from "@/components/ui-primitives";
import { copy, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { scopeHref, type Scope } from "@/lib/scope";
import type { CalendarEvent } from "./calendar-contract";

export type CalendarDriveSummaryEvent = CalendarEvent & {
  aggregated?: boolean;
  all_day?: boolean;
  summary_primary?: string;
  summary_secondary?: string;
  summary_tertiary?: string;
  shed_count?: number;
  vaccine_count?: number;
  drive_count?: number;
  catch_up_count?: number;
  scheduled_count?: number;
  deferred_count?: number;
  review_count?: number;
  shed_labels?: string[];
  vaccine_labels?: string[];
};

function MetaCard({
  icon: Icon,
  label,
  value,
}: {
  icon: typeof CalendarDays;
  label: string;
  value: string | number;
}) {
  return (
    <div
      style={{
        border: "1px solid var(--line2)",
        borderRadius: 16,
        padding: 14,
        background:
          "linear-gradient(180deg, color-mix(in srgb, var(--panel) 92%, white 8%), var(--panel))",
      }}
    >
      <div
        className="muted small"
        style={{ display: "flex", alignItems: "center", gap: 8 }}
      >
        <Icon className="ic" aria-hidden="true" style={{ width: 14, height: 14 }} />
        {label}
      </div>
      <div style={{ marginTop: 8, fontSize: 18, fontWeight: 700 }}>{value}</div>
    </div>
  );
}

export function CalendarDriveSummaryDrawer({
  event,
  closeHref,
  scope,
  pageContract,
}: {
  event: CalendarDriveSummaryEvent;
  closeHref: string;
  scope: Scope;
  pageContract: AdminUiPageContract;
}) {
  const statusLabel = optionLabel(pageContract, "calendar_status", event.status);
  const statusTone = optionTone(pageContract, "calendar_status", event.status) as
    | "ok"
    | "warn"
    | "dng"
    | "info"
    | "mut"
    | "pur"
    | "teal";
  const openHref = scopeHref("/vaccination", scope);
  const sheds = event.shed_labels ?? [];
  const vaccines = event.vaccine_labels ?? [];
  const isReview = event.event_type !== "vaccination_drive";

  return (
    <>
      <Link
        href={closeHref}
        replace
        className="veil"
        aria-label={copy(pageContract, "drawer.event.close_label")}
        scroll={false}
      />
      <aside className="drawer on" aria-label="Vaccination drive summary">
        <div className="dh">
          <span
            className="fic"
            style={{
              background:
                "color-mix(in srgb, var(--brand) 18%, var(--panel))",
              color: "var(--brand)",
            }}
          >
            <CalendarDays className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">Park drive</div>
            <h2>{event.title}</h2>
            <div className="muted small" style={{ marginTop: 4 }}>
              {event.subtitle || "All-day park drive"}
            </div>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link
            href={closeHref}
            replace
            className="iconbtn"
            aria-label={copy(pageContract, "drawer.event.close_label")}
            scroll={false}
          >
            <X className="ic" />
          </Link>
        </div>

        <div className="dc">
          <div className="chipset" style={{ marginBottom: 14 }}>
            <Tag tone={statusTone}>{statusLabel}</Tag>
            <Tag tone="info">All day</Tag>
            <Tag tone="mut">Park-level drive</Tag>
          </div>

          <div
            style={{
              border: "1px solid var(--line2)",
              borderRadius: 18,
              padding: 16,
              background:
                "linear-gradient(160deg, color-mix(in srgb, var(--brand) 10%, var(--panel)) 0%, var(--panel) 62%)",
            }}
          >
            <div style={{ fontSize: 20, fontWeight: 700 }}>
              {event.summary_primary || `${event.target_count} goats`}
            </div>
            {event.summary_secondary ? (
              <div className="muted" style={{ marginTop: 6, fontWeight: 600 }}>
                {event.summary_secondary}
              </div>
            ) : null}
            {event.summary_tertiary ? (
              <div className="muted small" style={{ marginTop: 8 }}>
                {event.summary_tertiary}
              </div>
            ) : null}
          </div>

          <div
            style={{
              marginTop: 16,
              display: "grid",
              gridTemplateColumns: "repeat(2, minmax(0, 1fr))",
              gap: 12,
            }}
          >
            <MetaCard
              icon={MapPinned}
              label="Sheds"
              value={(event.shed_count ?? sheds.length) || 0}
            />
            <MetaCard
              icon={Syringe}
              label="Vaccines"
              value={(event.vaccine_count ?? vaccines.length) || 0}
            />
            <MetaCard icon={Layers3} label={isReview ? "Items" : "Goats"} value={event.target_count} />
            <MetaCard
              icon={CalendarDays}
              label={isReview ? "Review queues" : "Drive packets"}
              value={isReview ? event.review_count ?? event.target_count : event.drive_count ?? 1}
            />
          </div>

          {sheds.length ? (
            <div style={{ marginTop: 18 }}>
              <div className="b700" style={{ marginBottom: 8 }}>
                Shed coverage
              </div>
              <div className="chipset">
                {sheds.map((shed) => (
                  <Tag key={shed} tone="mut">
                    {shed}
                  </Tag>
                ))}
              </div>
            </div>
          ) : null}

          {vaccines.length ? (
            <div style={{ marginTop: 18 }}>
              <div className="b700" style={{ marginBottom: 8 }}>
                Vaccine mix
              </div>
              <div className="chipset">
                {vaccines.map((vaccine) => (
                  <Tag key={vaccine} tone="info">
                    {vaccine}
                  </Tag>
                ))}
              </div>
            </div>
          ) : null}

          {(event.catch_up_count ||
            event.scheduled_count ||
            event.deferred_count ||
            event.review_count) ? (
            <div style={{ marginTop: 18 }}>
              <div className="b700" style={{ marginBottom: 8 }}>
                Queue breakdown
              </div>
              <div className="metagrid">
                <div>
                  <div className="k">Catch-up</div>
                  <div className="v">{event.catch_up_count ?? 0}</div>
                </div>
                <div>
                  <div className="k">Scheduled</div>
                  <div className="v">{event.scheduled_count ?? 0}</div>
                </div>
                <div>
                  <div className="k">Deferred</div>
                  <div className="v">{event.deferred_count ?? 0}</div>
                </div>
                <div>
                  <div className="k">Review items</div>
                  <div className="v">{event.review_count ?? 0}</div>
                </div>
              </div>
            </div>
          ) : null}

          <div style={{ marginTop: 20 }}>
            <Link href={openHref} className="btn p" scroll={false}>
              Open vaccination workspace
            </Link>
          </div>
        </div>
      </aside>
    </>
  );
}
