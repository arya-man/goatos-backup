"use client";

// Entry drawers for the datasets the retired Sales DB sheet used to carry: buyer leads (record +
// update call status), farmer-group leads, market quotes, sold-animal tag lists and weight checks.
//
// Same overlay mechanism as SalesRecordDrawer: CLIENT state driven by the ?panel= URL param
// (LocalOverlayLink changes history WITHOUT an RSC request), the mock's `.scrim`/`.drawer.on`
// anatomy, Escape/scrim/X close. Every visible string resolves from the backend page contract.

import { Banknote, X } from "lucide-react";
import { useCallback, useEffect, useRef, useSyncExternalStore } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { SalesBuyerLead, SalesFpoLead } from "@/lib/api/procurement";
import {
  recordBenchmarkAction,
  recordBuyerLeadAction,
  recordFpoLeadAction,
  recordSoldTagsAction,
  recordWeightCheckAction,
  updateBuyerLeadStatusAction,
  updateFpoLeadStatusAction,
} from "./sales-actions";

export const SALES_PANELS = ["buyer_leads", "fpo_leads", "quote", "tags", "weight_check"] as const;
export type SalesPanel = (typeof SALES_PANELS)[number];

function readPanelParam(): string {
  return new URL(window.location.href).searchParams.get("panel") ?? "";
}

function subscribeToOverlayUrl(onChange: () => void): () => void {
  window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
  window.addEventListener("popstate", onChange);
  return () => {
    window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
    window.removeEventListener("popstate", onChange);
  };
}

export function SalesPipelineDrawers({
  pageContract,
  listHref,
  panelHrefs,
  canRecord,
  buyerLeads,
  buyerStatusOptions,
  fpoLeads,
  fpoStatusOptions,
}: {
  pageContract: AdminUiPageContract;
  /** The list URL to restore on close (current farm/paging, no panel param). */
  listHref: string;
  /** The URL that reopens each panel — used as the lead forms' return_to so a save lands back
      inside its drawer. Plain data, precomputed by the server component: a function cannot cross
      the RSC boundary. */
  panelHrefs: Record<SalesPanel, string>;
  canRecord: boolean;
  /** First page of the buyer pipeline, newest first, server-fetched with the page. */
  buyerLeads: SalesBuyerLead[];
  buyerStatusOptions: string[];
  fpoLeads: SalesFpoLead[];
  fpoStatusOptions: string[];
}) {
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const selection = useSyncExternalStore(subscribeToOverlayUrl, readPanelParam, () => "");
  const panel = (SALES_PANELS as readonly string[]).includes(selection) ? (selection as SalesPanel) : null;
  const open = panel !== null && canRecord;

  const close = useCallback(() => {
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(listHref);
  }, [listHref]);

  useEffect(() => {
    if (!open) return;
    const frame = window.requestAnimationFrame(() => closeButtonRef.current?.focus());
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    document.addEventListener("keydown", onKey);
    return () => {
      window.cancelAnimationFrame(frame);
      document.removeEventListener("keydown", onKey);
    };
  }, [open, close]);

  const field = (key: string) => copy(pageContract, `field.${key}`);
  const uncontacted = copy(pageContract, "field.call_status.uncontacted");
  const farmOptions = optionGroup(pageContract, "sales_farms").filter((option) => option.key !== "all");

  const titles: Record<SalesPanel, string> = {
    buyer_leads: copy(pageContract, "drawer.add_lead.title"),
    fpo_leads: copy(pageContract, "drawer.add_fpo.title"),
    quote: copy(pageContract, "drawer.add_quote.title"),
    tags: copy(pageContract, "drawer.add_tags.title"),
    weight_check: copy(pageContract, "drawer.add_weight_check.title"),
  };
  const title = panel ? titles[panel] : "";

  // The optional-farm select shared by the lead form and the tag-list form.
  const farmSelect = (id: string) => (
    <div className="fld">
      <label htmlFor={id}>{field("farm")}</label>
      <select id={id} name="farm" defaultValue="">
        <option value="">—</option>
        {farmOptions.map((option) => (
          <option key={option.key} value={option.key}>
            {option.label}
          </option>
        ))}
      </select>
    </div>
  );

  // A per-row status updater: select + save, one form per lead.
  const statusRow = (
    lead: { id: string; name: string; detail: string; status: string | null | undefined },
    options: string[],
    action: typeof updateBuyerLeadStatusAction,
    returnTo: string,
  ) => (
    <form key={lead.id} action={action} className="sales-leadrow">
      <input type="hidden" name="lead_id" value={lead.id} />
      <input type="hidden" name="return_to" value={returnTo} />
      <span className="sales-leadname" title={lead.detail ? `${lead.name} · ${lead.detail}` : lead.name}>
        <b>{lead.name}</b>
        {lead.detail ? <span className="muted small"> · {lead.detail}</span> : null}
      </span>
      <select name="call_status" defaultValue={lead.status ?? ""} aria-label={field("call_status")}>
        <option value="">{uncontacted}</option>
        {options.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
        {lead.status && !options.includes(lead.status) ? <option value={lead.status}>{lead.status}</option> : null}
      </select>
      <button type="submit" className="btn sm">
        {copy(pageContract, "action.update_status")}
      </button>
    </form>
  );

  return (
    <>
      <div
        className={`scrim${open ? " on" : ""}`}
        aria-label={copy(pageContract, "action.close")}
        aria-hidden={!open}
        tabIndex={open ? 0 : -1}
        onClick={close}
      />
      <aside className={`drawer${open ? " on" : ""}`} aria-label={title} aria-hidden={!open} inert={!open}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--info)" }}>
            <Banknote className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "crumb")}</div>
            <h2>{title}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button
            ref={closeButtonRef}
            type="button"
            className="iconbtn"
            aria-label={copy(pageContract, "action.close")}
            onClick={close}
          >
            <X className="ic" aria-hidden="true" />
          </button>
        </div>

        {panel === "buyer_leads" ? (
          <div className="dc">
            <div className="mt">{copy(pageContract, "drawer.add_lead.new")}</div>
            <form action={recordBuyerLeadAction}>
              <input type="hidden" name="return_to" value={panelHrefs.buyer_leads} />
              <div className="note">{copy(pageContract, "required.hint.lead")}</div>
              <div className="fld">
                <label htmlFor="bl-buyer_name">{field("buyer_lead_name")}</label>
                <input id="bl-buyer_name" name="buyer_name" required maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="bl-buyer_place">{field("buyer_place")}</label>
                <input id="bl-buyer_place" name="buyer_place" maxLength={160} />
              </div>
              {farmSelect("bl-farm")}
              <div className="fld">
                <label htmlFor="bl-animal_type">{field("animal_type")}</label>
                <input id="bl-animal_type" name="animal_type" maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="bl-breed">{field("breed")}</label>
                <input id="bl-breed" name="breed" maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="bl-recorded_date">{field("recorded_date")}</label>
                <input id="bl-recorded_date" name="recorded_date" type="date" />
              </div>
              <div className="fld">
                <label htmlFor="bl-call_status">{field("call_status")}</label>
                <input id="bl-call_status" name="call_status" maxLength={160} list="bl-status-options" placeholder={uncontacted} />
                <datalist id="bl-status-options">
                  {buyerStatusOptions.map((option) => (
                    <option key={option} value={option} />
                  ))}
                </datalist>
              </div>
              <div className="df" style={{ padding: 0, border: 0, marginTop: 10 }}>
                <button type="submit" className="btn p">
                  {copy(pageContract, "action.save")}
                </button>
              </div>
            </form>
            <div className="mt">{copy(pageContract, "drawer.add_lead.recent")}</div>
            <div className="sales-leadlist">
              {buyerLeads.map((lead) =>
                statusRow(
                  {
                    id: lead.lead_id,
                    name: lead.buyer_name,
                    detail: [lead.buyer_place, lead.animal_type].filter(Boolean).join(" · "),
                    status: lead.call_status,
                  },
                  buyerStatusOptions,
                  updateBuyerLeadStatusAction,
                  panelHrefs.buyer_leads,
                ),
              )}
            </div>
          </div>
        ) : null}

        {panel === "fpo_leads" ? (
          <div className="dc">
            <div className="mt">{copy(pageContract, "drawer.add_fpo.new")}</div>
            <form action={recordFpoLeadAction}>
              <input type="hidden" name="return_to" value={panelHrefs.fpo_leads} />
              <div className="note">{copy(pageContract, "required.hint.fpo")}</div>
              <div className="fld">
                <label htmlFor="fp-fpo_name">{field("fpo_name")}</label>
                <input id="fp-fpo_name" name="fpo_name" required maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="fp-district">{field("district")}</label>
                <input id="fp-district" name="district" maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="fp-taluk">{field("taluk")}</label>
                <input id="fp-taluk" name="taluk" maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="fp-state">{field("state")}</label>
                <input id="fp-state" name="state" maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="fp-crops">{field("crops")}</label>
                <input id="fp-crops" name="crops" maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="fp-call_status">{field("call_status")}</label>
                <input id="fp-call_status" name="call_status" maxLength={160} list="fp-status-options" placeholder={uncontacted} />
                <datalist id="fp-status-options">
                  {fpoStatusOptions.map((option) => (
                    <option key={option} value={option} />
                  ))}
                </datalist>
              </div>
              <div className="df" style={{ padding: 0, border: 0, marginTop: 10 }}>
                <button type="submit" className="btn p">
                  {copy(pageContract, "action.save")}
                </button>
              </div>
            </form>
            <div className="mt">{copy(pageContract, "drawer.add_fpo.recent")}</div>
            <div className="sales-leadlist">
              {fpoLeads.map((lead) =>
                statusRow(
                  {
                    id: lead.lead_id,
                    name: lead.fpo_name,
                    detail: [lead.district, lead.state].filter(Boolean).join(" · "),
                    status: lead.call_status,
                  },
                  fpoStatusOptions,
                  updateFpoLeadStatusAction,
                  panelHrefs.fpo_leads,
                ),
              )}
            </div>
          </div>
        ) : null}

        {panel === "quote" ? (
          <form action={recordBenchmarkAction} style={{ display: "contents" }}>
            <div className="dc">
              <input type="hidden" name="return_to" value={listHref} />
              <div className="note">{copy(pageContract, "required.hint.quote")}</div>
              <div className="fld">
                <label htmlFor="q-breed">{field("breed")}</label>
                <input id="q-breed" name="breed" required maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="q-category">{field("quote_category")}</label>
                <input id="q-category" name="category" maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="q-market">{field("market")}</label>
                <input id="q-market" name="market" maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="q-source">{field("quote_source")}</label>
                <input id="q-source" name="source" maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="q-ex_farm_rate">{field("ex_farm_rate")}</label>
                <input id="q-ex_farm_rate" name="ex_farm_rate" maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="q-transport_rate">{field("transport_rate")}</label>
                <input id="q-transport_rate" name="transport_rate" maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="q-landing_cost_per_kg">{field("landing_cost_per_kg")}</label>
                <input id="q-landing_cost_per_kg" name="landing_cost_per_kg" type="number" min={0} step="0.01" />
              </div>
              <div className="fld">
                <label htmlFor="q-market_price_per_kg">{field("market_price_per_kg")}</label>
                <input id="q-market_price_per_kg" name="market_price_per_kg" type="number" min={0} step="0.01" />
              </div>
            </div>
            <div className="df">
              <button type="submit" className="btn p">
                {copy(pageContract, "action.save")}
              </button>
              <button type="button" className="btn" onClick={close}>
                {copy(pageContract, "action.cancel")}
              </button>
            </div>
          </form>
        ) : null}

        {panel === "tags" ? (
          <form action={recordSoldTagsAction} style={{ display: "contents" }}>
            <div className="dc">
              <input type="hidden" name="return_to" value={listHref} />
              <div className="note">{copy(pageContract, "drawer.add_tags.hint")}</div>
              {farmSelect("t-farm")}
              <div className="fld">
                <label htmlFor="t-tag_rows">{field("tag_rows")}</label>
                <textarea id="t-tag_rows" name="tag_rows" required rows={10} placeholder={copy(pageContract, "drawer.add_tags.example")} />
              </div>
            </div>
            <div className="df">
              <button type="submit" className="btn p">
                {copy(pageContract, "action.save")}
              </button>
              <button type="button" className="btn" onClick={close}>
                {copy(pageContract, "action.cancel")}
              </button>
            </div>
          </form>
        ) : null}

        {panel === "weight_check" ? (
          <form action={recordWeightCheckAction} style={{ display: "contents" }}>
            <div className="dc">
              <input type="hidden" name="return_to" value={listHref} />
              <div className="note">{copy(pageContract, "required.hint.weight_check")}</div>
              <div className="fld">
                <label htmlFor="w-tag_number">{field("tag_number")}</label>
                <input id="w-tag_number" name="tag_number" maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="w-book_weight_kg">{field("book_weight_kg")}</label>
                <input id="w-book_weight_kg" name="book_weight_kg" type="number" min="0.01" step="0.01" required />
              </div>
              <div className="fld">
                <label htmlFor="w-video_weight_kg">{field("video_weight_kg")}</label>
                <input id="w-video_weight_kg" name="video_weight_kg" type="number" min="0.01" step="0.01" required />
              </div>
              <div className="fld">
                <label className="sales-check">
                  <input type="checkbox" name="farm_born" />
                  <span>{field("farm_born")}</span>
                </label>
              </div>
            </div>
            <div className="df">
              <button type="submit" className="btn p">
                {copy(pageContract, "action.save")}
              </button>
              <button type="button" className="btn" onClick={close}>
                {copy(pageContract, "action.cancel")}
              </button>
            </div>
          </form>
        ) : null}
      </aside>
    </>
  );
}
