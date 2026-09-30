import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import { varAlpha } from "minimal-shared/utils";
import type { AnimalPurchaseAnimal } from "@/lib/api/procurement";
import { Tag } from "@/components/ui-primitives";
import { AnimalPurchaseLightbox } from "./animal-purchase-lightbox";
import { MEDIA_TILE_CAPTION_SX, MEDIA_TILE_EMPTY_SX, MEDIA_TILE_FIGURE_SX, MEDIA_TILES_SX } from "./animal-purchase-tile-sx";

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
  /** A capture the read could not resolve to a backend route and known MIME right now. */
  mediaEmpty: string;
  /** Accessible name of the proof tile that opens the selected capture. */
  photoOpen: string;
  /** Closes the enlarged capture. */
  close: string;
  /** Heading over the answers. */
  answersTitle: string;
  /** The web's name for the director's verdict section (the phone says "Your verdict" to the director). */
  verdictSection: string;
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
 * One row of capture tiles per slot. Tiles are byte-free on the card; clicking one opens the
 * selected photo/video in the in-page lightbox, where telemetry attributes the proof ref and kind.
 * The mime decides the tile kind; a capture without a backend route or known mime says so rather
 * than rendering a broken tag.
 */
export function AnimalPurchaseMedia({ slots, copy }: { slots: MediaSlot[]; copy: SopCopy }) {
  // Every capture with a backend proof route and a known mime is a tile, in the order the inspector
  // recorded them (slot order is the form's order); anything else says so in its place rather
  // than rendering a broken tag.
  const items = slots.flatMap((slot) =>
    slot.items.flatMap((item) =>
      item.media_url && (isImage(item) || isVideo(item))
        ? [
            {
              proofRef: item.proof_ref,
              url: item.media_url,
              thumbnailUrl: item.thumbnail_url ?? (isImage(item) ? item.media_url : undefined),
              kind: isImage(item) ? ("photo" as const) : ("video" as const),
              title: slot.title,
            },
          ]
        : [],
    ),
  );
  const missing = slots.flatMap((slot) => slot.items.filter((item) => !(item.media_url && (isImage(item) || isVideo(item)))).map((item) => ({ item, slot })));
  if (items.length === 0 && missing.length === 0) return null;
  return (
    <Box sx={MEDIA_TILES_SX}>
      <AnimalPurchaseLightbox items={items} openLabel={copy.photoOpen} closeLabel={copy.close} />
      {missing.map(({ item, slot }) => (
        <AnimalPurchaseEmptyTile key={item.proof_ref} text={copy.mediaEmpty} caption={slot.title} />
      ))}
    </Box>
  );
}

/** A capture with no playable proof: an outlined placeholder tile saying so, captioned like a real one. */
export function AnimalPurchaseEmptyTile({ text, caption }: { text: string; caption: string }) {
  return (
    <Box component="figure" sx={MEDIA_TILE_FIGURE_SX}>
      <Typography component="div" variant="caption" sx={MEDIA_TILE_EMPTY_SX}>
        {text}
      </Typography>
      <Typography component="figcaption" variant="caption" sx={MEDIA_TILE_CAPTION_SX}>
        {caption}
      </Typography>
    </Box>
  );
}

/**
 * The answers grouped by section in served order: the top block has no heading, every later
 * section is titled. An attention row carries a warn dot and its text in the warn colour so a
 * reject signal is visible at a glance among thirty rows.
 */
export function AnimalPurchaseAnswers({ rows, copy }: { rows: AnswerRow[]; copy: SopCopy }) {
  if (rows.length === 0) return null;
  // Web reading order (maintainer 2026-09-14): the breed sits right under the goat id, and the
  // director's verdict section is named as theirs -- on the phone the same section reads "Your
  // verdict" because the director is the one filling it.
  const breed = rows.find((row) => row.question_id === "breed");
  const ordered: AnswerRow[] = [];
  for (const row of rows) {
    if (row.question_id === "breed") continue;
    ordered.push(row);
    if (breed && row.question_id === "goat_id") ordered.push({ ...breed, section: row.section });
  }
  if (breed && !ordered.includes(breed) && !rows.some((row) => row.question_id === "goat_id")) ordered.push(breed);
  const sections: { section: string; rows: AnswerRow[] }[] = [];
  for (const row of ordered) {
    const section = row.question_id === "field_verdict" || row.section === rows.find((r) => r.question_id === "field_verdict")?.section ? copy.verdictSection : row.section;
    const last = sections[sections.length - 1];
    if (last && last.section === section) last.rows.push(row);
    else sections.push({ section, rows: [row] });
  }
  return (
    <Box
      sx={{
        minWidth: 0,
        display: "grid",
        gridTemplateColumns: "repeat(auto-fill, minmax(min(300px, 100%), 1fr))",
        columnGap: 3,
        rowGap: 0.75,
        alignItems: "start",
      }}
    >
      {sections.map((group, index) => (
        <Box component="section" key={`${index}-${group.section}`} sx={{ breakInside: "avoid", mb: 0.75, minWidth: 0 }}>
          {group.section ? <Typography component="h4" variant="overline" sx={SECTION_TITLE_SX}>{group.section}</Typography> : null}
          <Box component="dl" sx={{ m: 0, display: "flex", flexDirection: "column" }}>
            {group.rows.map((row) => (
              <Box key={row.question_id} data-question={row.question_id} data-attention={row.attention ? "" : undefined} sx={row.attention ? ATTENTION_ROW_SX : ANSWER_ROW_SX}>
                <Box component="dt" sx={{ color: row.attention ? "warning.main" : "text.secondary", display: "flex", gap: 0.875, alignItems: "baseline", minWidth: 0 }}>
                  {row.attention ? <Box component="span" data-attention-dot="" title={copy.attentionHint} role="img" aria-label={copy.attentionHint} sx={ATTENTION_DOT_SX} /> : null}
                  {row.question}
                </Box>
                <Box component="dd" sx={{ m: 0, fontWeight: "fontWeightSemiBold", textAlign: "right", overflowWrap: "anywhere", color: row.attention ? "warning.main" : undefined }}>{row.answer}</Box>
              </Box>
            ))}
          </Box>
        </Box>
      ))}
    </Box>
  );
}

const SECTION_TITLE_SX = {
  display: "block",
  m: 0,
  mb: 0.375,
  px: 0.625,
  pb: 0.5,
  color: "text.secondary",
  borderBottom: 1,
  borderColor: "divider",
} as const;

// One answer: the question left, the answer right, a hairline under every row but the last.
const ANSWER_ROW_SX = {
  display: "grid",
  gridTemplateColumns: "minmax(0, 1fr) minmax(72px, auto)",
  gap: 1.25,
  alignItems: "baseline",
  px: 0.625,
  py: 0.5,
  typography: "body2",
  borderBottom: 1,
  borderColor: "divider",
  "&:last-of-type": { borderBottom: 0 },
} as const;

// The reject signal: the row on a soft warning tint, its text in the warning colour.
const ATTENTION_ROW_SX = {
  ...ANSWER_ROW_SX,
  bgcolor: varAlpha("var(--palette-warning-mainChannel)", 0.09),
  borderRadius: 0.75,
} as const;

const ATTENTION_DOT_SX = {
  width: "calc(1 * var(--spacing))",
  height: "calc(1 * var(--spacing))",
  borderRadius: "50%",
  display: "inline-block",
  flex: "0 0 auto",
  alignSelf: "center",
  bgcolor: "warning.main",
} as const;
