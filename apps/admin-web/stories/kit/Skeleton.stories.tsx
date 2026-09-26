import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, within } from "storybook/test";
import {
  CardGridSkeleton,
  ChartCardSkeleton,
  ChipRowSkeleton,
  ControlsCardSkeleton,
  DetailCardSkeleton,
  FilterCardSkeleton,
  GridSkeleton,
  KanbanSkeleton,
  KpiRowSkeleton,
  ListCardSkeleton,
  PageHeaderSkeleton,
  PageSkeleton,
  StatStripSkeleton,
  TableSkeleton,
  TabsSkeleton,
  ToolbarSkeleton,
} from "@/components/app/skeletons";
import { Frame, mobile } from "../_fixtures/frame";

/**
 * The route / panel loading blocks (components/app/skeletons). Each one renders the SAME layout parts
 * as the block it stands in for (CustomBreadcrumbs anatomy, MUI Tabs, KpiGrid, Card + CardHeader,
 * Table, the template pagination toolbar, the job-list grid), so a loading.tsx composed from them
 * lines up with the page. Every block is aria-hidden: the play function asserts a screen reader is
 * not handed the placeholders.
 */
const meta: Meta = {
  title: "Kit/Skeleton",
  parameters: { layout: "fullscreen", dualTheme: { height: 900 } },
  decorators: [
    (Story) => (
      <Frame>
        <Story />
      </Frame>
    ),
  ],
};
export default meta;
type Story = StoryObj;

const assertHidden: Story["play"] = async ({ canvasElement }) => {
  const canvas = within(canvasElement);
  await expect(canvas.queryAllByRole("row")).toHaveLength(0);
  await expect(canvas.queryAllByRole("tab")).toHaveLength(0);
};

/** A list page: header + action, KPI deck, filter card, table card with pager. */
export const ListPage: Story = {
  render: () => (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} />
      <KpiRowSkeleton count={4} icon />
      <FilterCardSkeleton fields={[180, 180, "search"]} actions={1} />
      <TableSkeleton columns={6} rows={8} headerAction />
    </PageSkeleton>
  ),
  play: assertHidden,
};
export const ListPageMobile: Story = { ...ListPage, parameters: mobile };

/** An analytics page: header tabs, KPI deck with minis, chart cards two-up. */
export const AnalyticsPage: Story = {
  render: () => (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} tabs={<TabsSkeleton count={5} counts />} />
      <KpiRowSkeleton count={3} spark trend />
      <GridSkeleton
        items={[
          { size: { xs: 12, md: 6 }, node: <ChartCardSkeleton height={240} legend /> },
          { size: { xs: 12, md: 6 }, node: <ChartCardSkeleton height={240} /> },
        ]}
      />
    </PageSkeleton>
  ),
  play: assertHidden,
};
export const AnalyticsPageMobile: Story = { ...AnalyticsPage, parameters: mobile };

/** A card list page: stat strip, filter bar, job-list grid. */
export const CardListPage: Story = {
  render: () => (
    <PageSkeleton>
      <PageHeaderSkeleton actions={1} />
      <StatStripSkeleton count={4} />
      <ToolbarSkeleton left={<TabsSkeleton count={2} variant="pill" />} fields={[160, 160]} />
      <CardGridSkeleton count={6} />
    </PageSkeleton>
  ),
  play: assertHidden,
};
export const CardListPageMobile: Story = { ...CardListPage, parameters: mobile };

/** Detail page and board blocks. */
export const DetailAndBoard: Story = {
  render: () => (
    <PageSkeleton>
      <PageHeaderSkeleton actionWidths={[72, 72]} />
      <GridSkeleton
        items={[
          { size: { xs: 12, md: 8 }, node: <DetailCardSkeleton rows={4} columns={2} /> },
          { size: { xs: 12, md: 4 }, node: <ListCardSkeleton rows={4} /> },
        ]}
      />
      <ControlsCardSkeleton tabs={<TabsSkeleton count={3} counts />} toolbar={<FilterCardSkeleton inCard fields={["search", 140]} />} />
      <ChipRowSkeleton count={4} />
      <KanbanSkeleton lanes={[3, 2, 2]} />
    </PageSkeleton>
  ),
  play: assertHidden,
};
export const DetailAndBoardMobile: Story = { ...DetailAndBoard, parameters: mobile };
