/** One proof tile edge (px): a uniform square so a strip of captures reads as one row. */
export const MEDIA_TILE_SIZE = 112;

/** A tile row: the captures and their placeholders wrap on a phone. */
export const MEDIA_TILES_SX = { display: "flex", flexWrap: "wrap", columnGap: 1.75, rowGap: 1.5, minWidth: 0 } as const;

/** The figure around one tile: the square, then its two-line caption. */
export const MEDIA_TILE_FIGURE_SX = { m: 0, width: MEDIA_TILE_SIZE, display: "flex", flexDirection: "column", gap: 0.5 } as const;

export const MEDIA_TILE_CAPTION_SX = {
  color: "text.secondary",
  lineHeight: 1.25,
  display: "-webkit-box",
  WebkitLineClamp: 2,
  WebkitBoxOrient: "vertical",
  overflow: "hidden",
} as const;

/** A capture the read could not resolve: the dashed square with the backend's sentence in it. */
export const MEDIA_TILE_EMPTY_SX = {
  display: "grid",
  placeItems: "center",
  width: MEDIA_TILE_SIZE,
  height: MEDIA_TILE_SIZE,
  p: 1,
  border: 1,
  borderStyle: "dashed",
  borderColor: "divider",
  borderRadius: "var(--r-lg)",
  bgcolor: "background.neutral",
  color: "text.secondary",
  textAlign: "center",
} as const;
