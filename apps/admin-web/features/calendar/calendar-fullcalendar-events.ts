import type { EventInput } from "@fullcalendar/core";
import { ownerColor, type CalendarEvent, type OwnerPresentationMap } from "./calendar-contract";

// FullCalendar event adapter: CalendarEvent → EventInput.
//
// The event's `backgroundColor` is the owner tone (Mesha palette resolved via ownerColor()); the
// drive-vs-detail routing decision is carried on extendedProps.isDrive so the click handler in
// CalendarFullView can route drive rows to /calendar/drive/[id] and other rows to the local overlay
// drawer via #calendar_event=<id>. `subtitle`, `park_code`, `shed_name` and `owner_key` also ride
// extendedProps so the event card renderer can compose the same meta line the old EventRow showed.
export type CalendarFullCalendarExtendedProps = {
  isDrive: boolean;
  subtitle: string;
  park_code: string;
  shed_name: string;
  owner_key: string;
  status: string;
  summary_tertiary: string;
  event_type: string;
};

export function toFullCalendarEvents(
  events: CalendarEvent[],
  ownerMeta: OwnerPresentationMap,
): EventInput[] {
  return events.map((event) => {
    const tone = ownerColor(event.owner_key, ownerMeta);
    const isDrive = event.aggregated && event.event_type === "vaccination_drive";
    return {
      id: event.event_id,
      title: event.title,
      start: event.due_at,
      allDay: event.all_day,
      backgroundColor: tone,
      borderColor: tone,
      // FullCalendar uses `color` on the event token; keep both so tone lands both as fill and border.
      color: tone,
      extendedProps: {
        isDrive,
        subtitle: event.subtitle ?? "",
        park_code: event.park_code ?? "",
        shed_name: event.shed_name ?? "",
        owner_key: event.owner_key,
        status: event.status,
        summary_tertiary: event.summary_tertiary ?? "",
        event_type: event.event_type,
      } satisfies CalendarFullCalendarExtendedProps,
    };
  });
}
