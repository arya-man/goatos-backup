import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import { Label } from "@/components/minimal/label";
import { Canvas, MOBILE, Row, Stack } from "./_data";

/**
 * Chips / tags / pills on the template: MUI `Chip` (filter chips, applied filters) and the template
 * `Label` (status tags, counts). The legacy `.chip` / `.chipset` / `.tag` / `.pill` / `.achip` /
 * `.dimchip` theme classes are deleted; this file pins the template states that replaced them.
 */
const meta = { title: "Kit/Chips & Labels", parameters: { layout: "fullscreen" } } satisfies Meta;
export default meta;
type Story = StoryObj;

const Count = ({ n, on }: { n: number; on?: boolean }) => (
  <Label variant={on ? "filled" : "soft"} color={on ? "primary" : "default"} sx={{ ml: 0.75 }}>
    {n}
  </Label>
);

const FILTERS = [
  { label: "All pens", count: 164, on: true },
  { label: "Kids 0–3 mo", count: 46 },
  { label: "Growers", count: 101 },
  { label: "Breeding does", count: 17 },
];

export const FilterChips: Story = {
  render: () => (
    <Canvas>
      <Stack title="filter chips — default / selected / disabled / with count">
        <Row>
          {FILTERS.map((f) => (
            <Chip
              key={f.label}
              clickable
              variant={f.on ? "filled" : "outlined"}
              color={f.on ? "primary" : "default"}
              label={<>{f.label}<Count n={f.count} on={f.on} /></>}
            />
          ))}
          <Chip disabled variant="outlined" label={<>Quarantine<Count n={0} /></>} />
        </Row>
      </Stack>
      <Stack title="applied filters + clear">
        <Row>
          <Chip size="small" variant="soft" label="Park: Seletar" onDelete={() => {}} />
          <Chip size="small" variant="soft" label="Vendor: Kranji Livestock" onDelete={() => {}} />
          <Chip size="small" variant="soft" label="ADG < 150 g/day" onDelete={() => {}} />
          <Chip size="small" color="error" variant="soft" label="Clear all" onClick={() => {}} />
        </Row>
      </Stack>
    </Canvas>
  ),
};

export const StatusTags: Story = {
  render: () => (
    <Canvas>
      <Stack title="status tags">
        <Row>
          <Label color="success">On target</Label>
          <Label color="warning">Watch</Label>
          <Label color="error">Below target</Label>
          <Label color="info">Awaiting verification</Label>
          <Label color="primary">SF-048</Label>
          <Label variant="outlined">Draft</Label>
        </Row>
      </Stack>
      <Stack title="demographic chips">
        <Row>
          <Chip size="small" variant="soft" color="secondary" label={<>Does <b>92</b></>} />
          <Chip size="small" variant="soft" color="info" label={<>Bucks <b>72</b></>} />
          <Chip size="small" variant="soft" label={<>Untagged <b>4</b></>} />
        </Row>
      </Stack>
    </Canvas>
  ),
};

export const OverflowAndManyItems: Story = {
  render: () => (
    <Canvas>
      <Stack title="long label truncation">
        <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1, maxWidth: 320 }}>
          <Chip
            color="primary"
            title="Kranji Livestock Supply Cooperative — Batch 2026-04"
            label="Kranji Livestock Supply Cooperative — Batch 2026-04"
            sx={{ maxWidth: 1 }}
          />
          <Label color="info" sx={{ maxWidth: 200, display: "inline-block", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
            Lim Chu Kang Goat Breeders Association
          </Label>
        </Box>
      </Stack>
      <Stack title="many items — must wrap, never scroll the page sideways">
        <Row>
          {Array.from({ length: 28 }, (_, i) => {
            const on = i % 7 === 0;
            return (
              <Chip
                key={i}
                variant={on ? "filled" : "outlined"}
                color={on ? "primary" : "default"}
                label={<>Pen {String(i + 1).padStart(2, "0")}<Count n={100 + i * 3} on={on} /></>}
              />
            );
          })}
        </Row>
      </Stack>
      <Stack title="empty state">
        <Row>
          <Chip size="small" variant="soft" label="No filters applied" />
        </Row>
      </Stack>
    </Canvas>
  ),
};

export const Mobile: Story = { ...OverflowAndManyItems, ...MOBILE };
