import Link from "@/components/no-prefetch-link";
import { Tag } from "@/components/ui-primitives";
import type { HerdSignalItem } from "@/lib/api/herd-signals";
import { MAPPING_LABEL, MAPPING_TONE, fmtBleMac } from "./format";
import { herdSignalsHref, type HerdSignalsParams } from "./params";

// The Tag Mapping tab is NOT the live view with different filters: it answers "which BLE tag
// belongs to which animal identifier, who said so, and when", so it has its own ten columns
// (mock/herd-signals-mock.html, `renderMapping`). Rendering the live table here showed motion
// counts and battery on a screen about identity.
//
// Three of the mock's columns have NO field on the live contract today (HerdSignalItem in
// lib/api/herd-signals.ts): "Existing tag 1", "Existing tag 2" (the animal's non-BLE identifiers)
// and "Verified by" / "Verified at" (the mapping's provenance). They render "—". The names and
// dates in those columns of the reference design are fixture values, and reprinting one would
// state a verification that never happened. When the backend serves them, fill in the cell bodies
// here; the column set does not need to change.
const NOT_ON_CONTRACT = "—";

function AnimalCell({ item }: { item: HerdSignalItem }) {
  if (!item.display_id) {
    return (
      <>
        <b className="muted">{NOT_ON_CONTRACT}</b>
        <small>No animal identifier</small>
      </>
    );
  }
  return (
    <>
      <b>{item.display_id}</b>
      <small>{MAPPING_LABEL[item.mapping_state].toLowerCase()}</small>
    </>
  );
}

export function HerdSignalsMappingTable({
  items,
  nextCursor,
  params,
  tagsSeen,
}: {
  items: HerdSignalItem[];
  nextCursor: string | null;
  params: HerdSignalsParams;
  tagsSeen: number;
}) {
  const nf = (value: number) => value.toLocaleString("en-IN");
  // Cursor pagination has no back-pointer on the wire, so there is no honest "Previous" here: the
  // control that DOES exist is "back to the first page", and it is disabled on the first page
  // rather than mislabelled.
  const firstPageHref = herdSignalsHref(params, {});
  const nextHref = nextCursor ? herdSignalsHref(params, { hs_cursor: nextCursor }) : null;
  const onFirstPage = !params.cursor;

  const pager = (variant: "top" | "bottom") => (
    <div className={`pager${variant === "top" ? " pager-top" : ""}`}>
      {onFirstPage ? (
        <button type="button" className="pgbtn" disabled title="Already on the first page">
          &larr; First page
        </button>
      ) : (
        <Link href={firstPageHref} className="pgbtn">
          &larr; First page
        </Link>
      )}
      {nextHref ? (
        <Link href={nextHref} className="pgbtn">
          Next &rarr;
        </Link>
      ) : (
        <button type="button" className="pgbtn" disabled title="No further page for this filter">
          Next &rarr;
        </button>
      )}
      <span>
        Showing <b>{nf(items.length)}</b> rows of <b>{nf(tagsSeen)}</b> tags in scope
      </span>
    </div>
  );

  if (items.length === 0) {
    return (
      <div className="empty">
        <div className="eicon">
          <svg className="ic" viewBox="0 0 24 24">
            <path d="M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7-7l-1.7 1.7" />
            <path d="M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7 7L12.2 19" />
          </svg>
        </div>
        <h4>{params.mappingState ? "No tags in this mapping state" : "No BLE tags to map yet"}</h4>
        <p>
          {params.mappingState
            ? "Every tag in scope is in a different mapping state — an outcome, not a read failure."
            : "No BLE gateway has posted a tag for this tenant yet. Tags appear here the moment a gateway forwards one, mapped or not."}
        </p>
        {params.mappingState ? (
          <div className="eact">
            <Link href={herdSignalsHref(params, { hs_map: undefined })} className="btn sm">
              Show all tags
            </Link>
          </div>
        ) : null}
      </div>
    );
  }

  return (
    <>
      {pager("top")}
      <div className="tblwrap">
        <table className="resp">
          <thead>
            <tr>
              <th>Animal</th>
              <th>Existing tag 1</th>
              <th>Existing tag 2</th>
              <th>Smart tag capable</th>
              <th>BLE tag ID</th>
              <th>BLE MAC</th>
              <th>Source</th>
              <th>Verified by</th>
              <th>Verified at</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <tr key={item.tag_id}>
                <td data-l="Animal" className="wide animcell">
                  <AnimalCell item={item} />
                </td>
                <td data-l="Existing tag 1" className="faint">
                  {NOT_ON_CONTRACT}
                </td>
                <td data-l="Existing tag 2" className="faint">
                  {NOT_ON_CONTRACT}
                </td>
                <td data-l="Smart tag capable">
                  {item.mapping_state === "mapped" ? <Tag tone="ok">Yes</Tag> : <Tag tone="mut">No</Tag>}
                </td>
                <td data-l="BLE tag ID">
                  <span className="mono">{item.tag_id}</span>
                </td>
                <td data-l="BLE MAC">
                  <span className="mono faint">{fmtBleMac(item.tag_mac)}</span>
                </td>
                {/* The only provenance the live contract carries: which gateway forwarded the tag.
                    A tag with no gateway id was not attributed to one, so it gets "—", never a
                    guessed source. */}
                <td data-l="Source">{item.gateway_id ? "Gateway" : <span className="faint">{NOT_ON_CONTRACT}</span>}</td>
                <td data-l="Verified by" className="faint">
                  {NOT_ON_CONTRACT}
                </td>
                <td data-l="Verified at" className="faint">
                  {NOT_ON_CONTRACT}
                </td>
                <td data-l="Status">
                  <Tag tone={MAPPING_TONE[item.mapping_state]}>{MAPPING_LABEL[item.mapping_state]}</Tag>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {pager("bottom")}
    </>
  );
}
