import { Syringe } from "lucide-react";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { Tag, type Tone } from "@/components/ui-primitives";
import {
  copy,
  optionLabel,
  optionTone,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import type { LiveTrackerCombo } from "@/lib/api/vaccination-live-tracker";

// Combo doses — one proof, two obligations.
//
// The animal cell renders the REAL scanned identifier. The mock's `GT-#####` ids were generated, and
// reproducing that pattern would put a fabricated animal number in front of a farm director; where a
// goat is dual-tagged the second tag is carried in the cell's title rather than invented into a
// column that has nowhere to go.
export function LiveTrackerComboCard({
  combo,
  passportHref,
  truncatedHref,
  pageContract,
}: {
  combo: LiveTrackerCombo;
  passportHref: (goatId: string) => string;
  truncatedHref: string;
  pageContract: AdminUiPageContract;
}) {
  const placeholder = copy(pageContract, "label.placeholder");
  return (
    <section id="lt-combo" className="card lt-card" style={{ scrollMarginTop: 80 }}>
      <div className="hd">
        <Syringe className="ic" style={{ color: "var(--purple)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.combo.title")}</h3>
        {combo.vaccine_labels.length > 0 ? (
          <span className="lt-combo-chip">{combo.vaccine_labels.join(" + ")}</span>
        ) : null}
        <div className="sp" style={{ flex: 1 }} />
        <span className="small muted">
          {combo.animal_count} {copy(pageContract, "section.combo.count_suffix")}
        </span>
      </div>
      <div className="bd">
        <div className="note" style={{ marginBottom: 11 }}>
          {copy(pageContract, "section.combo.note")}
        </div>

        {combo.rows.length === 0 ? (
          <div className="lt-empty" style={{ padding: "6px 0" }}>
            <div style={{ minWidth: 0, flex: 1 }}>
              <b style={{ fontSize: 14 }}>{copy(pageContract, "section.combo.empty_title")}</b>
              <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
                {copy(pageContract, "section.combo.empty_body")}
              </span>
            </div>
          </div>
        ) : (
          <>
            <div className="lt-comborow lt-comborow-head">
              <span>{copy(pageContract, "section.combo.header_animal")}</span>
              <span>{copy(pageContract, "section.combo.header_shed")}</span>
              <span>{copy(pageContract, "section.combo.header_proof")}</span>
              <span>{copy(pageContract, "section.combo.header_doses")}</span>
            </div>
            {combo.rows.map((row) => {
              const dualTagTitle = row.secondary_tag ? `${row.primary_tag} · ${row.secondary_tag}` : row.primary_tag;
              return (
                <div key={row.goat_id} className="lt-comborow">
                  <span>
                    <LocalOverlayLink
                      href={passportHref(row.goat_id)}
                      className="lt-goatid"
                      scroll={false}
                      title={dualTagTitle || row.display_id}
                      aria-label={`${copy(pageContract, "drawer.passport.aria")} — ${row.display_id || row.primary_tag}`}
                    >
                      {row.primary_tag || row.display_id || placeholder}
                    </LocalOverlayLink>
                  </span>
                  <span>{row.shed_label || placeholder}</span>
                  <span>
                    <Tag tone={optionTone(pageContract, "live_proof_state", row.proof_state) as Tone}>
                      {optionLabel(pageContract, "live_proof_state", row.proof_state)}
                    </Tag>
                  </span>
                  <span className="lt-dosecell">
                    {row.doses.map((dose) => (
                      <Tag key={dose.obligation_id} tone={optionTone(pageContract, "live_dose_state", dose.state) as Tone}>
                        {dose.vaccine_label} · {optionLabel(pageContract, "live_dose_state", dose.state)}
                      </Tag>
                    ))}
                  </span>
                </div>
              );
            })}
          </>
        )}

        <div style={{ marginTop: 9 }}>
          {/* The mock's "All 70 combo animals →" implies a longer list behind the card. It is only a
              real destination when the server actually truncated; otherwise every combo animal is
              already on screen and the button says so instead of leading nowhere. */}
          {combo.rows_truncated ? (
            <LocalOverlayLink href={truncatedHref} className="btn sm" scroll={false}>
              {combo.animal_count} {copy(pageContract, "action.all_combo_animals")}
            </LocalOverlayLink>
          ) : (
            // Not truncated: the count is already in the card header, and repeating it here reads as
            // "0 All combo animals" on a day with none. The button keeps its place and states why it
            // does nothing.
            <span className="btn sm" aria-disabled="true" title={copy(pageContract, "section.combo.all_listed")}>
              {copy(pageContract, "action.all_combo_animals")}
            </span>
          )}
        </div>
      </div>
    </section>
  );
}
