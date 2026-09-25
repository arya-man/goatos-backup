package app

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

func TestBootstrapBodyMirrorsResponseFields(t *testing.T) {
	var got []string
	for _, typ := range []reflect.Type{reflect.TypeOf(bootstrapHead{}), reflect.TypeOf(struct {
		Pages []domain.PageContract `json:"pages"`
	}{}), reflect.TypeOf(bootstrapTail{})} {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			got = append(got, f.Name+" "+f.Type.String()+" "+string(f.Tag))
		}
	}
	var want []string
	typ := reflect.TypeOf(domain.BootstrapResponse{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		want = append(want, f.Name+" "+f.Type.String()+" "+string(f.Tag))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bootstrapHead/bootstrapTail drifted from domain.BootstrapResponse:\n got %v\nwant %v", got, want)
	}
}

func TestBootstrapBodyMatchesEncodedResponse(t *testing.T) {
	inputs := []BootstrapInput{
		{TenantID: "tenant-1", ActorID: "actor-1"},
		{TenantID: "tenant-1", ActorID: "actor-2", Grants: []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "tenant-1"}}},
		{TenantID: "tenant-1", ActorID: "actor-3", Grants: []permissions.ActiveGrant{{Role: permissions.RoleProcurementDirector, ScopeType: "tenant", ScopeID: "tenant-1"}}},
	}
	for _, input := range inputs {
		svc := NewService()
		etag, body, err := svc.BootstrapBody(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		resp := svc.Bootstrap(context.Background(), input)
		var want bytes.Buffer
		if err := json.NewEncoder(&want).Encode(resp); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(body, want.Bytes()) {
			t.Fatalf("body differs from Encode(resp) for %s: %d vs %d bytes", input.ActorID, len(body), want.Len())
		}
		if etag != resp.CachePolicy.ETag || etag == "" {
			t.Fatalf("etag %q vs %q", etag, resp.CachePolicy.ETag)
		}
		if resp.FamilyHashes["pages"] != hashStruct(resp.Pages) {
			t.Fatalf("pages hash drifted from hashStruct(resp.Pages)")
		}
	}
}
