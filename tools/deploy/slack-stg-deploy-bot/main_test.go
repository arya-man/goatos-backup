package main

import (
	"strings"
	"testing"
)

func TestNextAndroidVersionFileBumpsPatchAndCode(t *testing.T) {
	input := `
val releaseVersionCode = (
    project.findProperty("goatosVersionCode") as String?
        ?: System.getenv("GOATOS_ANDROID_VERSION_CODE")
    )
    ?.takeIf { it.isNotBlank() }
    ?.toInt()
    ?: 22
val releaseVersionName = (
    project.findProperty("goatosVersionName") as String?
        ?: System.getenv("GOATOS_ANDROID_VERSION_NAME")
    )
    ?.takeIf { it.isNotBlank() }
    ?: "0.1.21"
`

	updated, next, err := nextAndroidVersionFile(input)
	if err != nil {
		t.Fatalf("nextAndroidVersionFile returned error: %v", err)
	}
	if next.Name != "0.1.22" || next.Code != 23 {
		t.Fatalf("next version = %s (%d), want 0.1.22 (23)", next.Name, next.Code)
	}
	if !strings.Contains(updated, `?: 23`) {
		t.Fatalf("updated content did not contain bumped versionCode:\n%s", updated)
	}
	if !strings.Contains(updated, `?: "0.1.22"`) {
		t.Fatalf("updated content did not contain bumped versionName:\n%s", updated)
	}
	if strings.Contains(updated, "4608d477") {
		t.Fatalf("updated content contains a commit-derived version name:\n%s", updated)
	}
}
