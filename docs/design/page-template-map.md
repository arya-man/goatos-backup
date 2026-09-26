# Page → template map

Every admin-web route is composed from the licensed MUI Minimal template (Minimal_TypeScript_v7.7.0
`next-ts`). This file names, per route, the template page it copies and the template section
components each block is built from. Template section code lives verbatim under
`apps/admin-web/components/minimal/` (each file listed in `template-sources.json`).

`design:guard` rule `page-template-map` reads the table rows below: column 3 lists the feature files
that render the route (paths relative to `apps/admin-web`), column 4 the template modules those files
must import (as `@/<module>`). A mapped page that stops importing one of its sections fails the guard.

| Route | Template page | Feature files | Template section modules | Blocks |
|---|---|---|---|---|
| `/sales/sold` | Ecommerce overview (`/dashboard/ecommerce`) | `features/procurement/sales-sold.tsx`, `features/procurement/sales-sold-monthly.tsx` | `components/minimal/sections/overview/e-commerce/ecommerce-widget-summary`, `components/minimal/sections/overview/e-commerce/ecommerce-sales-overview`, `components/minimal/sections/overview/e-commerce/ecommerce-yearly-sales`, `components/minimal/sections/overview/e-commerce/ecommerce-latest-products`, `components/minimal/sections/overview/e-commerce/ecommerce-best-salesman`, `components/minimal/table`, `components/minimal/label` | KPIs → EcommerceWidgetSummary · Sold by weight → EcommerceSalesOverview · Month by month → EcommerceYearlySales · Price per kg by breed → EcommerceLatestProducts · Buyers → EcommerceBestSalesman · Deals → order-list table anatomy |

Pages in this table are also held by `page-template-no-pastel`: no `KpiCard variant="tint"/"gradient"`
or `AnalyticsWidgetSummary` in their feature files.

## Block detail

### `/sales/sold` → Ecommerce overview

| Block | Template section | Grid |
| --- | --- | --- |
| Headline figures (revenue, animals, realized price per kg, manure; feed when sold) | `EcommerceWidgetSummary` (title, figure, sparkline, detail line in the trend row) | `{xs:12, lg:8}` column, cards `{xs:12, sm:6}` |
| Sold animals by weight (four bands, share, weight provenance) | `EcommerceSalesOverview` (progress rows, share %) | `{xs:12, lg:4}` right rail |
| Month by month (revenue / animals / manure, one scale at a time) | `EcommerceYearlySales` (CardHeader + ChartSelect, legend totals, area chart) | `{xs:12, lg:8}` |
| Price per kg by breed | `EcommerceLatestProducts` (rounded avatar list) | `{xs:12, lg:4}` right rail |
| Buyers (paged) | `EcommerceBestSalesman` (TableHeadCustom, avatar lead, soft Label share chip) + template pagination | `12` |
| Deals ledger (server-paged, row opens the read-only deal drawer) | Order list anatomy: Card + CardHeader count Label, TableHeadCustom, soft status Label, pagination | `12` |
| Farm chips | shared template Tabs (owned by TABS) | above the grid |
