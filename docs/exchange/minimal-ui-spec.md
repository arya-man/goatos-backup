# Minimal (MUI Store) dashboard — builder spec

Source: minimals.cc v7 (dashboard routes need a login, so they redirect to /auth/*/sign-in).
[M] = measured from computed styles on public pages (/components/mui/*, sign-in).
[K] = Minimal theme defaults (theme source), not re-measured live. Brand colour = `primary`, which you swap.

## 0. Neutral tokens
- [M] text.primary `#1C252E` (rgb 28,37,46); text.secondary `#637381`; text.disabled `#919EAB`
- grey: 100 `#F9FAFB` · 200 `#F4F6F8` [M: table head bg] · 300 `#DFE3E8` · 400 `#C4CDD5` · 500 `#919EAB` · 600 `#637381` · 700 `#454F5B` · 800 `#1C252E` · 900 `#141A21`
- [M] divider / outline `rgba(145,158,171,0.2)`; hover `rgba(145,158,171,0.08)`; selected `rgba(145,158,171,0.16)`
- background.default `#FFFFFF`, background.paper `#FFFFFF`, background.neutral `#F4F6F8`
- Status (soft label bg = colour @16% alpha, text = the colour's dark shade): success `#22C55E`, warning `#FFAB00`, error `#FF5630`, info `#00B8D9`
- Radius: base 8 [M: inputs] · card 16 [M] · chip/label 6 · avatar 50% · dialog 16 · popover 10
- Spacing unit 8px

## 1. Type
- [M] Body: "Public Sans Variable", system-ui fallbacks, 16/24 base
- [M] Headings h1–h3: **Barlow** 800/700 (h3 = 24px/700 at md). h4–h6 and subtitles: Public Sans
- Scale (desktop): h1 64/80 800 · h2 48/64 800 · h3 32/48 700 · h4 24/36 700 · h5 20/30 700 · h6 18/28 600 (mobile h3 24, h5 18 [M])
- subtitle1 16/24 600 · subtitle2 14/22 600 · body1 16/24 · body2 14/22 · caption 12/18 · overline 12/18 700 UPPER
- button 14/24 700, no uppercase (textTransform: none)

## 2. App shell
- [K] Sidebar 300px (mini variant 88px). Paper bg white, right border 1px dashed `rgba(145,158,171,0.12)`
- Logo block: 40px mark, padding 20px 20px 8px. Workspace switcher lives in the header, not the sidebar
- Subheader: overline 11px/700 UPPER, `text.disabled`, padding 16px 8px 8px 12px. Hovering it shows a chevron; clicking collapses the group
- Nav item: height 44, radius 8, padding 4px 8px 4px 12px, gap 12 (icon to label), icon 24px, label 14/500, `text.secondary`, 4px vertical gap between items
- Active item: bg primary @8%, text + icon primary.main, label 600; hover primary @16%. Sub-items: 36h, dot/tree-line bullet, active sub-item = text.primary 600 (no bg)
- Optional nav footer: upgrade card with avatar
- [M] Header 64px on mobile, 72px from lg. Transparent at top; after scroll: bg white @80% + `backdrop-filter: blur(6px)` and height shrinks by 8
- Header, left to right: workspace switcher (logo + name + Label "Free/Pro" + chevron), then spacer, then search pill ("Search… ⌘K", grey bg, radius 8), language flag, notifications (badge), contacts, settings gear (spinning), and avatar 40px with a conic gradient ring (primary→warning) + 2px gap
- ≤1200px (lg): sidebar hidden, burger in header opens a temporary Drawer (same nav, 300px, backdrop `rgba(28,37,46,0.48)`)
- Content: maxWidth lg (1200) container, padding x 16 / sm 24 / lg 40; top padding 8 (below the header)

## 3. Page header (CustomBreadcrumbs)
- Title h4 (24/36 700), then breadcrumbs 14px below it with an 8px gap
- Breadcrumbs: body2; links text.primary; current item text.disabled; separator = 4px round dot `text.disabled` with 16px margin either side
- Action button right-aligned on the header row: contained "inherit" (grey.800 bg, white text), radius 8, height 36, start icon "+" (e.g. "New user")
- Header margin-bottom 40px (mb 5)

## 4. Card
- [M] radius 16, shadow `0 0 2px 0 rgba(145,158,171,0.2), 0 12px 24px -4px rgba(145,158,171,0.12)`, bg white, no border
- CardHeader padding 24px 24px 0; title h6 (18/28 600); subheader body2 text.secondary, 4px under the title; action = IconButton "⋮" or small select
- CardContent padding 24. Section dividers: 1px dashed divider

## 5. KPI widgets
- App "WidgetSummary": card, padding 24, row layout. Left: subtitle2 title; h3 value (Barlow 32/48); trend row = 24px circle icon (success/error @16% bg) + "+2.6%" subtitle2 + "last 7 days" body2 secondary. Right: sparkline bar chart 84×56, primary colour
- Banking/Analytics variant: card with a soft gradient bg (colour lighter→light @48%), 48px icon top-left, a trend chip top-right (↗ +2.6%), value h4, title subtitle2 @64% opacity, full-width area sparkline at the bottom. Faint shape-square background SVG
- Banking "Total balance" hero: card with primary.darker gradient + dark overlay image, white text. Balance h3, eye toggle to mask, 2-row carousel of cards (VISA / Mastercard) with dots; actions row: Send / Add card / Request round buttons
- Grid: `Grid container spacing 3` (24px); 4-up at md for KPIs

## 6. List page (user/list, invoice/list)
- Card wraps everything (no outer padding)
- Status tabs row: Tabs with a 2px primary indicator (text.primary colour), tab label subtitle2 14/600, gap 40, each tab ends with a soft Label count (All = filled grey.800 when active, otherwise soft). Row bottom shadow `inset 0 -2px 0 0 rgba(145,158,171,0.08)`, px 20 (2.5)
- Toolbar: padding 20 (2.5) / right 8. Filters: role Select 200px wide (multi-select with checkboxes) + search TextField flex-1 with a start search icon + "⋮" menu (Print/Import/Export)
- Invoice list: a stat strip above the tabs — 5 circular progress icons + total/count/amount, split by vertical dashed dividers
- Active filters strip ("12 results found" + deletable chips + "Clear" error button)
- [M] Head cell: bg `#F4F6F8`, colour `#637381`, 14px/600, padding 16, no border. Sort label arrows
- [M] Body cell: 14px, padding 16, bottom border `1px dashed rgba(145,158,171,0.2)`; default row ~73px with avatar, dense ~53
- Name cell: Avatar 40 + stack (name = subtitle2 link, text.primary, underline on hover; email = body2 text.disabled)
- Label (soft): height 24, min-width 24, px 6, radius 6, 12px/700, bg colour@16%, text colour.dark (e.g. success `#118D57` on `rgba(34,197,94,0.16)`; warning `#B76E00`; error `#B71D18`; default grey text.secondary on `rgba(145,158,171,0.16)`)
- Row actions: edit (quick-edit dialog) pencil IconButton + "⋮" → popover (Edit / Delete in error colour). Checkbox column 48px; row selected bg primary @8%
- Bulk-select bar: replaces the head, primary.lighter bg, "N selected" + delete icon
- [M] Footer 64px: "Dense" switch on the left (small); on the right "Rows per page" select + "1–5 of 20" + chevrons, 14px, top border dashed

## 7. Forms (user/new, account)
- Grid: left col md=4 = avatar card (padding 80px 24px 40px, status Label top-right); right col md=8 = form card padding 24
- Upload avatar: 144px circle with a 1px dashed outline + 8px padding; inner grey placeholder "Upload photo" camera overlay on hover (black @64%). Caption "Allowed *.jpeg… max 3 Mb" caption text.disabled, centred, mt 24
- Below the avatar: switches with a label + description (e.g. "Email verified"), label subtitle2, desc body2 secondary
- Fields: `display grid, gridTemplateColumns: 1fr 1fr (sm), rowGap 24, columnGap 16`
- [M] TextField outlined: height 56 (small 40), radius 8, border `rgba(145,158,171,0.2)` → hover text.primary → focus 2px text.primary (not brand); floating label 16→12 shrink, focused label text.primary 600
- Select = same shell with a chevron. Autocomplete country with flags
- Switch: MUI Minimal custom — track 33×20? (thumb 14, track radius 10, checked track primary, unchecked grey @48%)
- Submit: contained "inherit" (grey.800) lg/md right-aligned at the bottom, mt 24; loading = LoadingButton
- Account page: Tabs with icons (General / Billing / Notifications / Social / Security) above the same 4/8 layout
- Section form pattern (product/new): left column md=4 title h6 + body2 secondary description, right column md=8 card

## 8. Dialog / snackbar / popover
- Dialog: radius 16, shadow `-40px 40px 80px -8px rgba(0,0,0,0.24)`, padding title 24 (h6), content px 24, actions 24 with gap 12. Confirm dialog: title h6, content body1, actions Cancel (outlined inherit) + a colour action (contained error)
- Backdrop `rgba(28,37,46,0.48)`
- Snackbar (Sonner): white paper, radius 12, padding 4px 16px 4px 4px, left 48px icon tile (colour @8% bg, icon in the colour), text subtitle2, shadow z8. Top-right, stacked
- Popover/Menu: radius 10, padding 4, shadow `0 0 2px 0 rgba(145,158,171,0.24), -20px 20px 40px -4px rgba(145,158,171,0.24)`, **frosted bg** (white @90% + blur 20 + cyan/red corner blurs), arrow tick. MenuItem 14px, radius 6, padding 6px 8px, gap 16 icon, 4px between items

## 9. Invoice details (statement page)
- Header: breadcrumbs + a toolbar row: left icon buttons (Edit, View, Print, Download, Send, Share), right a status Select (Paid/Pending/Overdue/Draft) 150px
- One Card, padding 40 (5): row 1 logo 48 left, right = status Label + h6 invoice no.
- 2-col grid: "Invoice from" / "Invoice to" (subtitle2 label, body2 address), then "Date create" / "Due date"
- Items table: #, Description (title subtitle2 + desc body2 secondary, max 560), Qty, Unit price, Total (right-aligned); scrollable
- Totals block right-aligned: Subtotal / Shipping (error −) / Discount / Taxes / **Total** subtitle1; rows body2 w 160
- Footer: dashed divider, then "NOTES" subtitle2 + body2 left and "Have a question?" + email right
- The print/PDF preview opens in a full-screen dialog

## 10. Motion
- Transitions: theme durations shorter 200 / short 250 / standard 300, easing `cubic-bezier(0.4,0,0.2,1)`
- Nav items: bg-colour transition 150ms; the group collapse uses Collapse height
- Buttons: hover darkens + a coloured shadow (`0 8px 16px 0 primary@24%`) for contained colours
- IconButton ripple-ish scale via `varHover` (framer-motion whileHover scale 1.09, whileTap 0.97) on header icons; the settings gear spins (8s)
- Cards/tables: row hover bg `rgba(145,158,171,0.08)`
- Page enter: framer-motion fade/`varFade` inUp on landing only; the dashboard has a top NProgress bar in primary
- Header blur fades in on scroll; the drawer slides 225ms
