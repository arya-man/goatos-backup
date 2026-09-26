import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { Canvas, MOBILE, Row, Stack } from "./_data";

/**
 * Chips / tags / pills are theme CSS classes (`.chip`, `.chipset`, `.tag`, `.pill`, `.achip`,
 * `.dimchip`) rather than kit components, so this file pins their rendered states.
 */
const meta = { title: "Kit/Chips & Labels", parameters: { layout: "fullscreen" } } satisfies Meta;
export default meta;
type Story = StoryObj;

export const FilterChips: Story = {
  render: () => (
    <Canvas>
      <Stack title="chipset — default / selected / disabled / with count">
        <div className="chipset">
          <button type="button" className="chip on">All pens<span className="cbq">164</span></button>
          <button type="button" className="chip">Kids 0–3 mo<span className="cbq">46</span></button>
          <button type="button" className="chip">Growers<span className="cbq">101</span></button>
          <button type="button" className="chip">Breeding does<span className="cbq">17</span></button>
          <button type="button" className="chip" aria-disabled="true" disabled>Quarantine<span className="cbq">0</span></button>
        </div>
      </Stack>
      <Stack title="applied filters (.achip) + clear">
        <div className="fchipsbar">
          <span className="achip">Park: Seletar <b>×</b></span>
          <span className="achip">Vendor: Kranji Livestock <b>×</b></span>
          <span className="achip">ADG &lt; 150 g/day <b>×</b></span>
          <span className="achip clr">Clear all</span>
        </div>
      </Stack>
    </Canvas>
  ),
};

export const StatusTags: Story = {
  render: () => (
    <Canvas>
      <Stack title="status tags">
        <Row>
          <span className="tag" style={{ background: "var(--success-soft)", color: "var(--success-ink)" }}>On target</span>
          <span className="tag" style={{ background: "var(--warning-soft)", color: "var(--warning-ink)" }}>Watch</span>
          <span className="tag" style={{ background: "var(--error-soft)", color: "var(--error-ink)" }}>Below target</span>
          <span className="tag" style={{ background: "var(--info-soft)", color: "var(--info-ink)" }}>Awaiting verification</span>
          <span className="tag" style={{ background: "var(--primary-soft)", color: "var(--primary-ink)" }}>SF-048</span>
          <span className="pill">Draft</span>
        </Row>
      </Stack>
      <Stack title="demographic chips">
        <div className="dimchips">
          <span className="dimchip f">Does <b>92</b></span>
          <span className="dimchip m">Bucks <b>72</b></span>
          <span className="dimchip">Untagged <b>4</b></span>
        </div>
      </Stack>
    </Canvas>
  ),
};

export const OverflowAndManyItems: Story = {
  render: () => (
    <Canvas>
      <Stack title="long label truncation">
        <div className="chipset" style={{ maxWidth: 320 }}>
          <span className="chip on" title="Kranji Livestock Supply Cooperative — Batch 2026-04">Kranji Livestock Supply Cooperative — Batch 2026-04</span>
          <span className="tag" style={{ maxWidth: 200, overflow: "hidden", textOverflow: "ellipsis", display: "inline-block", whiteSpace: "nowrap", background: "var(--info-soft)", color: "var(--info-ink)" }}>
            Lim Chu Kang Goat Breeders Association
          </span>
        </div>
      </Stack>
      <Stack title="many items — must wrap, never scroll the page sideways">
        <div className="chipset">
          {Array.from({ length: 28 }, (_, i) => (
            <span key={i} className={`chip${i % 7 === 0 ? " on" : ""}`}>Pen {String(i + 1).padStart(2, "0")}<span className="cbq">{100 + i * 3}</span></span>
          ))}
        </div>
      </Stack>
      <Stack title="empty state">
        <div className="chipset"><span className="achip clr">No filters applied</span></div>
      </Stack>
    </Canvas>
  ),
};

export const Mobile: Story = { ...OverflowAndManyItems, ...MOBILE };
