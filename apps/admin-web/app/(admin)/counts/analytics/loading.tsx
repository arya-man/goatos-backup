import { ChartCardSkeleton, ControlRowSkeleton, FieldSkeleton, GridSkeleton, KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, StackSkeleton } from "@/components/app/skeletons";

/**
 * /counts/analytics: header + export, date range, six EcommerceWidgetSummary cards, the flow chart
 * (lg 8) beside the sex radial (lg 4), then breed mix beside the stacked stage / age / park mixes.
 */
export default function Loading() {
  return (
    <PageSkeleton gap={3} root="">
      {/* "Herd Analytics" is short: on a phone Export sits beside it. */}
      <PageHeaderSkeleton crumbLink={false} titleWidth={150} actionWidths={[89]} />
      {/* The small Window select: full width on a phone (44px tap floor), 300 x 56 from md. */}
      <ControlRowSkeleton>
        <FieldSkeleton width={{ xs: "100%", sm: 300 }} height={{ xs: 44, md: 56 }} />
      </ControlRowSkeleton>
      {/* KpiWidget course cards (no weekly series is served, so no ecommerce sparkline); only the
          second carries a caption line. */}
      <KpiRowSkeleton count={6} shapes={[{}, { hint: true }, {}, {}, {}, {}]} />
      <GridSkeleton
        items={[
          { size: { xs: 12, lg: 8 }, node: <ChartCardSkeleton height={364} /> },
          { size: { xs: 12, lg: 4 }, node: <ChartCardSkeleton height={364} /> },
        ]}
      />
      <GridSkeleton
        items={[
          { size: { xs: 12, md: 6 }, node: <ChartCardSkeleton height={520} /> },
          {
            size: { xs: 12, md: 6 },
            node: (
              <StackSkeleton>
                <ChartCardSkeleton height={160} />
                <ChartCardSkeleton height={100} />
              </StackSkeleton>
            ),
          },
        ]}
      />
    </PageSkeleton>
  );
}
