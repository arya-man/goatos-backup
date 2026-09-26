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
| `/approvals` | order/view/order-list-view (+ overview/course KPI row) | `features/approvals/approvals-page.tsx`, `features/approvals/approvals-queue-table.tsx` | `components/minimal/widgets/course-widget-summary`, `components/minimal/table`, `components/minimal/label`, `components/minimal/scrollbar` | CustomBreadcrumbs header; KPI row = 4 CourseWidgetSummary (Grid spacing 3, sm 6 / md 3); one Card: request-type Tabs, toolbar (status, farm, date), Scrollbar + Table with TableHeadCustom, soft Labels for type/status; TablePaginationLinks cursor footer; review drawer |
| `/alerts` | order/view/order-list-view (+ overview/course KPI row) | `features/alerts/alerts-page.tsx` | `components/minimal/widgets/course-widget-summary`, `components/minimal/table`, `components/minimal/label`, `components/minimal/scrollbar`, `components/minimal/iconify` | Header + Configure action; 3 CourseWidgetSummary; Card: severity Tabs with Label counts, toolbar (park, day stepper), Alerts, TableHeadCustom table with severity Label |
| `/leave` | user/view/user-list-view (+ overview/course KPI row) | `features/leave/leave-page.tsx` | `components/minimal/widgets/course-widget-summary`, `components/minimal/table`, `components/minimal/label` | 4 status CourseWidgetSummary; approvals queue Card (CardHeader + Label count, toolbar, TableHeadCustom); list Card with status Tabs + Label counts, TableHeadCustom, status Label |
| `/routines` | user/view/user-list-view (+ overview/course KPI row) | `features/pen-routines/routines-page.tsx` | `components/minimal/widgets/course-widget-summary`, `components/minimal/table`, `components/minimal/label`, `components/minimal/iconify` | 5 CourseWidgetSummary; routines Card + Today tasks Card (CardHeader + Label count, toolbar, TableHeadCustom), soft Labels; routine drawer |
| `/people` | user/view/user-list-view (+ overview/course KPI row on the Clock tab) | `features/people/people-page.tsx`, `features/people/people-board.tsx`, `features/people/clock-screen.tsx` | `components/minimal/sections/user/user-table-row`, `components/minimal/table`, `components/minimal/label`, `components/minimal/scrollbar`, `components/minimal/widgets/course-widget-summary` | CustomBreadcrumbs + "Add person" action; module Tabs; directory Card: status Tabs with Label count, UserTableToolbar-shaped filter row, Scrollbar + TableHeadCustom + UserTableRow (avatar, name, email), TablePaginationLinks; Clock tab: CourseWidgetSummary bucket tiles, toolbar, TableHeadCustom table with flag Labels |
| `/calendar` | calendar/view/calendar-view | `features/calendar/calendar.tsx`, `features/calendar/calendar-full-view.tsx` | `components/minimal/calendar`, `components/minimal/link-button` | Heading row + Upcoming/History action; owner filter chips (CalendarFiltersResult row); one Card: workstream Tabs, CalendarRoot + CalendarToolbar + FullCalendar; event drawer |
| `/verify` | invoice/view/invoice-list-view (analytics drawer: overview/e-commerce + overview/analytics) | `features/verification-review/verification-review-page.tsx`, `features/verification-review/oversight-analytics.tsx`, `features/verification-review/toxin-review-list.tsx` | `components/minimal/sections/invoice/invoice-analytic`, `components/minimal/table`, `components/minimal/label`, `components/minimal/scrollbar`, `components/minimal/sections/overview/e-commerce/ecommerce-sales-overview`, `components/minimal/sections/overview/analytics/analytics-website-visits` | Breadcrumbs + Analytics / Video log / Randomization actions; summary Card of InvoiceAnalytic per status (backend counts) with dashed dividers; list Card: status Tabs + Label counts, toolbar (module, subcategories, capture date, pen, Apply, Clear), Scrollbar + TableHeadCustom (URL sort), invoice-style rows (Avatar thumbnail, soft Labels), TablePaginationLinks; analytics drawer: BankingWidgetSummary tiles, EcommerceSalesOverview (backlog by module, age shape), AnalyticsWebsiteVisits (14-day verdicts vs arrivals), verifier table. /verification and /actions redirect here. |

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
