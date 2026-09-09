# Mobile worklist filter bar: Weighing tasks and Preventive Care rounds

**Maintainer request, 2026-09-10.** The phone's Weighing → Tasks list and the Preventive Care
planner list (Deworming, Ticks Removal, Hoof Trimming, Hair Trimming) had only an Active /
Completed pair and no way to narrow by date or pen. Both now carry the SAME filter bar above
the cards, and nothing below the bar changed.

## The bar

| Control | Default | Wire |
| --- | --- | --- |
| Pending / Completed pills | Pending | weighing `status=pending\|completed`; rounds `filter=active\|completed` |
| Date window (calendar) | today → today + 7 days | `date_from`, `date_to` (inclusive business dates, Asia/Kolkata) |
| Pen | All pens | `shed_id` + `partition_label` (the pen as the backend names it) |

The calendar is the app's own month grid: tap a start day then an end day for a range, tap
the SAME day twice for one day; quick picks Today / Next 7 days / Next 30 days / Last 7 days;
Clear returns to the default window. The Pen sheet lists the backend's pen vocabulary grouped
by park, each with its count of work in the window, and applies on one tap.

## What the backend owns

- **Counts on the pills are whole-filter**, narrowed by the window and the pen, never by the
  page and never by the status tab itself (each pill is its own bucket).
- **The pen vocabulary is whole-window and status-blind**, so a pen picked on Pending is still
  offered on Completed. It is empty on a read without a window (an APK predating the bar).
- **Carry rule.** A Pending read whose window starts ON OR BEFORE TODAY also carries still-open
  work dated before the window: a delayed task keeps its original planned date and must not
  vanish behind "today onwards". A window starting AFTER today is asked about alone. Completed
  is always the window exactly. `today` is the server's business date, filled by the service.
- **Order.** Pending reads soonest-first (upcoming work reads forward); Completed keeps
  newest-first. Cards are grouped under date headers when the window spans more than one day.
- **Legacy reads are untouched.** Without the new parameters both endpoints return exactly what
  they returned before (order, rows, no pens).

Weighing: `domain.CampaignListFilter`, `Repository.listCampaigns` / `campaignCounts` /
`campaignPens`. Preventive Care: `ports.ListRoundCardsQuery` (`DateFrom`, `DateTo`,
`PenShedID`, `PenPartitionKey`, `Today`), `roundCardsPageSQL` / `roundCardCountsSQL` /
`roundCardPensSQL`. A pen filter keeps a round card WHOLE (the card that holds the pen, with
every pen it holds); filtering the pen rows would shrink a three-pen round to one pen and
re-roll its status.

## What the phone owns

`core-ui/filters`: `WorklistFilterBar`, `WorklistDateWindowSheet`, `WorklistPenSheet` and the
`WorklistDateWindow` / `WorklistPen` model. Each distinct filter is its own Room cache scope
(weighing keyset rows + a pen blob in the weighing JSON cache; rounds one cached page per
filter), so switching back to a window shows its cards at once while the refresh runs behind.
The round list now pages on scroll (tail-window prefetch, bounded at 100 cards) because the
pill can honestly say 45 while the old single 20-card read could not show them.

Pinned by `TestListCampaignsFilterBarStatusMatrixPaginationAndOneToManyPen` and
`TestRoundCardsFilterBarStatusMatrixPaginationAndOneToManyPen` (Postgres, run against an
OCI throwaway database), and proven on the Realme phone against a throwaway clone of the
OCI database on 2026-09-10.
