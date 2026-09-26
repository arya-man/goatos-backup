import Box from "@mui/material/Box";

import { PageHeaderSkeleton, PageSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { DEFAULT_LIMIT, SALES_CONFIG_TABS } from "@/features/procurement/sales-config-layout";

/** /sales/config: header + Record sale / Tag animals, the account tabs, the Sales tab's ledger card. */
export default function Loading() {
  return (
    <PageSkeleton>
      <PageHeaderSkeleton
        actions={2}
        tabs={
          <Box sx={{ mb: 2 }}>
            <TabsSkeleton count={SALES_CONFIG_TABS.length} counts />
          </Box>
        }
      />
      {/* Contract table "sales-deals": 9 columns. */}
      <TableSkeleton columns={9} rows={DEFAULT_LIMIT} subheader headerAction />
    </PageSkeleton>
  );
}
