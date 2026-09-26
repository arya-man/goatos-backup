package domain

import "strings"

// PageProjection selects how much of each page the bootstrap carries.
//
// The full contract is ~1 MB, 98% of it pages[]: every page's copy, option groups, tables and
// drawers, for all 52 pages, on every read -- and admin-web reads it once per SSR render for the
// shell (which only needs navigation, route labels, chrome copy and each page's href) and once
// more for the ONE page it is rendering. So the API offers two narrower views of the SAME shape:
//
//   - PageProjectionSummary: every page keeps its identity fields (route_id, href, path_pattern,
//     title, subtitle, surface_kind, source_scope, migration_status) and carries EMPTY sections,
//     tables, drawers, controls, copy, option_groups and validation_notes. ~70 KB.
//   - PageProjectionSingle(route_id): that page in full, every other page as in Summary.
//
// The field set of every page is IDENTICAL in all three views (the generated client type does not
// change); only the contents of the heavy arrays/maps are withheld. A reader that needs a page's
// copy asks for that page. The ETag carries the projection so a 304 can never hand a summary
// where a page was asked for.
type PageProjection struct {
	// Summary withholds the heavy fields of every page.
	Summary bool
	// RouteID, when set, is the one page carried in full.
	RouteID string
}

// PageProjectionFull is the unprojected contract (no query parameter).
var PageProjectionFull = PageProjection{}

// IsFull reports whether nothing is withheld.
func (p PageProjection) IsFull() bool { return !p.Summary && p.RouteID == "" }

// ETagSuffix distinguishes the projected entity from the full one.
func (p PageProjection) ETagSuffix() string {
	switch {
	case p.RouteID != "":
		return ";page=" + p.RouteID
	case p.Summary:
		return ";pages=summary"
	default:
		return ""
	}
}

// ProjectPages returns a copy of resp with pages reduced per p. resp is never mutated: the
// service caches the full response and hands the same value to every caller.
func ProjectPages(resp BootstrapResponse, p PageProjection) BootstrapResponse {
	if p.IsFull() {
		return resp
	}
	out := resp
	out.Pages = make([]PageContract, len(resp.Pages))
	for i, page := range resp.Pages {
		if p.RouteID != "" && page.RouteID == p.RouteID {
			out.Pages[i] = page
			continue
		}
		out.Pages[i] = summarizePage(page)
	}
	if out.CachePolicy.ETag != "" {
		out.CachePolicy.ETag = withETagSuffix(out.CachePolicy.ETag, p.ETagSuffix())
	}
	return out
}

func summarizePage(page PageContract) PageContract {
	return PageContract{
		RouteID:         page.RouteID,
		Href:            page.Href,
		PathPattern:     page.PathPattern,
		Title:           page.Title,
		Subtitle:        page.Subtitle,
		SurfaceKind:     page.SurfaceKind,
		SourceScope:     page.SourceScope,
		Sections:        []Section{},
		Tables:          []TableContract{},
		Drawers:         []DrawerContract{},
		Controls:        []Control{},
		Copy:            map[string]string{},
		OptionGroups:    []OptionGroup{},
		MigrationStatus: page.MigrationStatus,
		ValidationNotes: []string{},
	}
}

// withETagSuffix keeps the ETag a valid quoted opaque tag: the suffix goes INSIDE the quotes.
func withETagSuffix(etag, suffix string) string {
	if suffix == "" {
		return etag
	}
	weak := strings.HasPrefix(etag, "W/")
	body := strings.TrimPrefix(etag, "W/")
	if len(body) >= 2 && body[0] == '"' && body[len(body)-1] == '"' {
		body = body[:len(body)-1] + suffix + `"`
	} else {
		body = body + suffix
	}
	if weak {
		return "W/" + body
	}
	return body
}
