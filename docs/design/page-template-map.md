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
