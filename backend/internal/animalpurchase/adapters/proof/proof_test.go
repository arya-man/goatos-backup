package proof

import "testing"

func TestPreviewURLUsesPhotoRouteButNeverVideoRoute(t *testing.T) {
	const route = "/app/proofs/11111111-1111-1111-1111-111111111111/download"

	if got := previewURL(route, "image/jpeg", nil); got != route {
		t.Fatalf("photo preview URL = %q, want route", got)
	}
	if got := previewURL(route, "video/mp4", nil); got != "" {
		t.Fatalf("video preview URL = %q, want empty without backend poster", got)
	}
	if got := previewURL(route, "video/mp4", map[string]any{"poster_url": "https://cdn.example/poster.jpg"}); got != "https://cdn.example/poster.jpg" {
		t.Fatalf("video poster URL = %q", got)
	}
	if got := previewURL(route, "video/mp4", map[string]any{"poster_url": route}); got != "" {
		t.Fatalf("video proof route poster URL = %q, want empty", got)
	}
	if got := previewURL(route, "video/mp4", map[string]any{"poster_url": "https://storage.googleapis.com/goatos-stg-media/proof.mp4"}); got != "" {
		t.Fatalf("video storage object poster URL = %q, want empty", got)
	}
	if got := previewURL(route, "video/mp4", map[string]any{"poster_url": "https://cdn.example/proof.mp4"}); got != "" {
		t.Fatalf("video-looking poster URL = %q, want empty", got)
	}
}
