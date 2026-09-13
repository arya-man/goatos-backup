import type { AnimalPurchaseAnimal } from "@/lib/api/procurement";
import { Tag } from "@/components/ui-primitives";
import { AnimalPurchaseLightbox } from "./animal-purchase-lightbox";

/**
 * The questionnaire half of an animal purchase card: the captures per media slot and the
 * Procurement SOP answers, both rendered verbatim from the review read. The backend already
 * decided the section order, the question text, the answer text and which answers the SOP reads
 * as a reject signal; this file only lays them out densely enough that a reviewer sees one animal
 * without scrolling a full screen.
 *
 * Legacy rows (questionnaire_version 0) never reach these components -- the page keeps its
 * single-video branch for them.
 */

export type SopCopy = {
  /** Heading over the media column. */
  mediaTitle: string;
  /** A capture the read could not sign a link for right now. */
  mediaEmpty: string;
  /** Accessible name of a photo thumbnail link. */
  photoOpen: string;
  /** Closes the enlarged capture. */
  close: string;
  /** Heading over the answers. */
  answersTitle: string;
  /** Title text of the attention marker. */
  attentionHint: string;
  /** Title text of the field-verdict chip. */
  fieldVerdictHint: string;
};

type MediaSlot = AnimalPurchaseAnimal["media_slots"][number];
type MediaItem = MediaSlot["items"][number];
type AnswerRow = AnimalPurchaseAnimal["answer_rows"][number];

function isImage(item: MediaItem): boolean {
  return (item.media_mime ?? "").startsWith("image/");
}

function isVideo(item: MediaItem): boolean {
  return (item.media_mime ?? "").startsWith("video/");
}

/** The director's own recommendation, beside the CEO's decision chip; absent when not recorded. */
export function FieldVerdictChip({ animal, hint }: { animal: AnimalPurchaseAnimal; hint: string }) {
  if (!animal.field_verdict || !animal.field_verdict_label) return null;
  return (
    <Tag tone={animal.field_verdict === "selected" ? "ok" : "warn"} title={hint}>
      {animal.field_verdict_label}
    </Tag>
  );
}

/**
 * One row of captures per slot: videos play inline (metadata only until the reviewer presses
 * play), photos are thumbnails that open the full image in a new tab, exactly as the toxin review
 * does. Both are the same height so a slot's items sit side by side. The mime decides the element;
 * a capture with neither a signed link nor a known mime says so rather than rendering a broken tag.
 */
export function AnimalPurchaseMedia({ slots, copy }: { slots: MediaSlot[]; copy: SopCopy }) {
  const shown = slots.filter((slot) => slot.items.length > 0);
  if (shown.length === 0) return null;
  return (
    <div className="ap-sop-media">
      <div className="bt">{copy.mediaTitle}</div>
      {shown.map((slot) => {
        // Every capture with a signed link and a known mime is a lightbox item, in the order the
        // inspector recorded it; anything else says so rather than rendering a broken tag.
        const items = slot.items.flatMap((item) =>
          item.media_url && (isImage(item) || isVideo(item))
            ? [{ proofRef: item.proof_ref, url: item.media_url, kind: isImage(item) ? ("photo" as const) : ("video" as const), title: slot.title }]
            : [],
        );
        const missing = slot.items.filter((item) => !(item.media_url && (isImage(item) || isVideo(item))));
        return (
          <div key={slot.slot} className="ap-sop-slot" data-slot={slot.slot}>
            <div className="ap-sop-slot-title muted small">{slot.title}</div>
            <div className="ap-sop-slot-items">
              <AnimalPurchaseLightbox items={items} openLabel={copy.photoOpen} closeLabel={copy.close} />
              {missing.map((item) => (
                <div key={item.proof_ref} className="ap-sop-media-empty muted small">
                  {copy.mediaEmpty}
                </div>
              ))}
            </div>
          </div>
        );
      })}
    </div>
  );
}

/**
 * The answers grouped by section in served order: the top block has no heading, every later
 * section is titled. An attention row carries a warn dot and its text in the warn colour so a
 * reject signal is visible at a glance among thirty rows.
 */
export function AnimalPurchaseAnswers({ rows, copy }: { rows: AnswerRow[]; copy: SopCopy }) {
  if (rows.length === 0) return null;
  const sections: { section: string; rows: AnswerRow[] }[] = [];
  for (const row of rows) {
    const last = sections[sections.length - 1];
    if (last && last.section === row.section) last.rows.push(row);
    else sections.push({ section: row.section, rows: [row] });
  }
  return (
    <div className="ap-sop-answers">
      <div className="bt">{copy.answersTitle}</div>
      <div className="ap-sop-sections">
        {sections.map((group, index) => (
          <section key={`${index}-${group.section}`} className="ap-sop-section">
            {group.section ? <h4 className="ap-sop-section-title">{group.section}</h4> : null}
            <dl className="ap-sop-rows">
              {group.rows.map((row) => (
                <div
                  key={row.question_id}
                  className={row.attention ? "ap-sop-row attention" : "ap-sop-row"}
                  data-question={row.question_id}
                >
                  <dt>
                    {row.attention ? <span className="dot l" title={copy.attentionHint} aria-label={copy.attentionHint} /> : null}
                    {row.question}
                  </dt>
                  <dd>{row.answer}</dd>
                </div>
              ))}
            </dl>
          </section>
        ))}
      </div>
    </div>
  );
}
