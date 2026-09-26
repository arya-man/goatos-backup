import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import * as React from "react";

import { RegisterPreview, type RegisterPreviewGroup } from "@/features/configuration/register-preview";
import { Frame } from "./_fixtures/frame";

/**
 * /configuration/items rendered from FIXTURE data — the register rail, the register table card
 * with its toolbar (search, ref filter, status tabs), status chips and the pager. The live route
 * needs `/admin/configuration/registers`, which the local stack cannot serve until its DB carries
 * migration 000348; this story is where the presentation is judged meanwhile. The copy mirrors
 * the backend keys the page reads; nothing here is new vocabulary.
 */
const copy = {
  crumb: "Items and settings",
  title: "Items and settings",
  "rail.title": "Registers",
  "column.display": "Name",
  "column.counts": "Holds",
  "column.status": "Status",
  "status.active": "Active",
  "status.archived": "Archived",
  "status.all": "All",
  "search.placeholder": "Search",
  "empty.rows": "Nothing here yet.",
  "pager.noun": "rows on this page",
  "pager.of": "of",
  "tag.builtin": "built in",
  "action.create_row.label": "Add",
  "action.previous": "Previous page",
  "action.next": "Next page",
  "sheet.title": "Sheet",
};

const groups: RegisterPreviewGroup[] = [
  { key: "places", label: "Farm places", items: [{ key: "parks", label: "Farms", count: 2, active: true }, { key: "pens", label: "Pens", count: 97 }] },
  { key: "animals", label: "Animal types", items: [{ key: "species", label: "Species", count: 2 }, { key: "breeds", label: "Breeds", count: 14 }, { key: "sexes", label: "Sexes", count: 2 }, { key: "stages", label: "Lifecycle stages", count: 11 }] },
  { key: "catalogues", label: "Catalogues", items: [{ key: "items", label: "Items", count: 38 }, { key: "vendors", label: "Vendors", count: 9 }] },
];

const farmColumns = [
  { key: "code", label: "Code", mono: true },
  { key: "state", label: "State" },
  { key: "capacity", label: "Capacity", numeric: true },
  { key: "active_since", label: "Active since" },
];
const farmRows = [
  { id: "p-cbe", cells: { name: "Coimbatore", code: "CBE", state: "Tamil Nadu", capacity: "1,200", active_since: "01/03/2024" }, status: "active" as const, builtin: true, counts: "61 pens · 857 animals" },
  { id: "p-cpt", cells: { name: "Channapatna", code: "CPT", state: "Karnataka", capacity: "900", active_since: "15/01/2025" }, status: "active" as const, counts: "36 pens · 716 animals" },
  { id: "p-hsr", cells: { name: "Hosur", code: "HSR", state: "Tamil Nadu", capacity: "300", active_since: "01/06/2023" }, status: "archived" as const, counts: "—" },
];

const meta: Meta = { title: "Pages/Configuration items", parameters: { layout: "fullscreen" } };
export default meta;
type Story = StoryObj;

export const Farms: Story = {
  render: () => (
    <Frame>
      <RegisterPreview copy={copy} groups={groups} title="Farms" total={2} columns={farmColumns} rows={farmRows.filter((r) => r.status === "active")} status="active" filters={[{ label: "State", value: "All" }]} />
    </Frame>
  ),
};

export const EmptyRegister: Story = {
  render: () => (
    <Frame>
      <RegisterPreview copy={copy} groups={groups.map((g) => ({ ...g, items: g.items.map((i) => ({ ...i, active: i.key === "breeds" })) }))} title="Breeds" total={0} columns={[{ key: "species", label: "Species" }]} rows={[]} />
    </Frame>
  ),
};

export const Mobile: Story = {
  ...Farms,
  globals: { viewport: { value: "mobile1", isRotated: false } },
};
