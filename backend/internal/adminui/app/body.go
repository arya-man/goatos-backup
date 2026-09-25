package app

import (
	"bytes"
	"encoding/json"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
)

// bootstrapHead and bootstrapTail are domain.BootstrapResponse split around "pages", with the
// same JSON names and order. encodeBootstrapBody writes head, the already-encoded pages, then
// tail, which is byte-for-byte what encoding the whole response gives.
// TestBootstrapBodyMirrorsResponseFields fails when a field is added to the response and not here.
type bootstrapHead struct {
	Source           string                     `json:"source"`
	SchemaVersion    string                     `json:"schema_version"`
	ContractRevision string                     `json:"contract_revision"`
	FamilyHashes     map[string]string          `json:"family_hashes"`
	CachePolicy      domain.ContractCachePolicy `json:"cache_policy"`
	Navigation       domain.NavigationContract  `json:"navigation"`
	NavChrome        string                     `json:"nav_chrome"`
	RouteLabels      []domain.RouteLabelRule    `json:"route_labels"`
	TopBar           domain.TopBarContract      `json:"top_bar"`
	RoleLenses       []domain.RoleLensContract  `json:"role_lenses"`
}

type bootstrapTail struct {
	Copy         map[string]string    `json:"copy"`
	DisplayRules []domain.DisplayRule `json:"display_rules"`
}

// encodeBootstrapBody returns what json.NewEncoder(w).Encode(resp) writes, reusing pagesJSON
// (json.Marshal(resp.Pages)) instead of encoding the pages again. Nil pagesJSON encodes resp whole.
func encodeBootstrapBody(resp domain.BootstrapResponse, pagesJSON []byte) ([]byte, error) {
	if pagesJSON == nil {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(resp); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	head, err := json.Marshal(bootstrapHead{
		Source:           resp.Source,
		SchemaVersion:    resp.SchemaVersion,
		ContractRevision: resp.ContractRevision,
		FamilyHashes:     resp.FamilyHashes,
		CachePolicy:      resp.CachePolicy,
		Navigation:       resp.Navigation,
		NavChrome:        resp.NavChrome,
		RouteLabels:      resp.RouteLabels,
		TopBar:           resp.TopBar,
		RoleLenses:       resp.RoleLenses,
	})
	if err != nil {
		return nil, err
	}
	tail, err := json.Marshal(bootstrapTail{Copy: resp.Copy, DisplayRules: resp.DisplayRules})
	if err != nil {
		return nil, err
	}
	const pagesKey = `,"pages":`
	out := make([]byte, 0, len(head)+len(pagesKey)+len(pagesJSON)+len(tail)+2)
	out = append(out, head[:len(head)-1]...) // drop the closing brace
	out = append(out, pagesKey...)
	out = append(out, pagesJSON...)
	out = append(out, ',')
	out = append(out, tail[1:]...) // drop the opening brace
	out = append(out, '\n')
	return out, nil
}
