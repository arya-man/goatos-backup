// Feed mix bar-chart layout, kept free of React/MUI so node --test can pin it.
//
// The card draws one horizontal bar per feed with the feed's name as the category label. Two
// round-2 defects (PR #294 O1) came from the label and the row disagreeing about how tall a row is:
// a name wrapped to three lines inside a ~23px row printed over its neighbours ("Mesha Adult /
// Concentrate / Mesha Kids Sheep / Concentrate" in one block), while truncating to one line cut
// "Mesha Adult Concentrate Goat" / "... Sheep" to three identical prefixes (round-1 D2). So a name
// takes AT MOST two lines, the last line keeps the END of the name (the word that tells two feeds
// apart) and is cut only past `width`, and the chart is made tall enough for two lines per row.

/** Characters per label line: phone gives the plot more room, a laptop card has room for more. */
export const FEED_MIX_LABEL_WIDTH = { phone: 18, laptop: 24 } as const;
/** Lines a feed name may take on the axis; past this the second line is cut at its end. */
export const FEED_MIX_MAX_LINES = 2;
/** Pixel pitch of one axis-label line, and the gap between two rows' labels. */
const LINE_PX = 15;
const ROW_GAP_PX = 10;
/** Chart chrome outside the plot rows: the wrapper's vertical padding plus the value axis. */
const CHROME_PX = 80;
/** The template card's own chart height; never shorter than this. */
export const FEED_MIX_MIN_HEIGHT = 360;

/**
 * A feed name broken at word boundaries into at most `maxLines` lines of about `width` characters.
 * Words that do not fit on the last line are folded into it and the line is cut at its END with
 * an ellipsis, so the leading words that start the name always read.
 */
export function feedMixLabelLines(label: string, width: number, maxLines: number = FEED_MIX_MAX_LINES): string[] {
  const words = label.split(/\s+/).filter(Boolean);
  if (words.length === 0) return [label];
  const lines: string[] = [];
  for (const word of words) {
    const last = lines[lines.length - 1];
    if (last !== undefined && (last.length + 1 + word.length <= width || lines.length === maxLines)) {
      lines[lines.length - 1] = `${last} ${word}`;
    } else {
      lines.push(word);
    }
  }
  const tail = lines[lines.length - 1];
  if (tail.length > width) lines[lines.length - 1] = `${tail.slice(0, Math.max(1, width - 1)).trimEnd()}…`;
  return lines;
}

/** Chart height that gives every row room for its tallest label. */
export function feedMixChartHeight(labels: string[][]): number {
  const lines = Math.max(1, ...labels.map((l) => l.length));
  const rowPx = lines * LINE_PX + ROW_GAP_PX;
  return Math.max(FEED_MIX_MIN_HEIGHT, labels.length * rowPx + CHROME_PX);
}
