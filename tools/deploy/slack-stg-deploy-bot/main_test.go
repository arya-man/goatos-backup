package main

import (
	"encoding/json"
	"os"
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
    ?: "1.0.0"
`

	updated, next, err := nextAndroidVersionFile(input)
	if err != nil {
		t.Fatalf("nextAndroidVersionFile returned error: %v", err)
	}
	if next.Name != "1.0.1" || next.Code != 23 {
		t.Fatalf("next version = %s (%d), want 1.0.1 (23)", next.Name, next.Code)
	}
	if !strings.Contains(updated, `?: 23`) {
		t.Fatalf("updated content did not contain bumped versionCode:\n%s", updated)
	}
	if !strings.Contains(updated, `?: "1.0.1"`) {
		t.Fatalf("updated content did not contain bumped versionName:\n%s", updated)
	}
	if strings.Contains(updated, "4608d477") {
		t.Fatalf("updated content contains a commit-derived version name:\n%s", updated)
	}
}

func TestPostDeployPanelStatesIdle(t *testing.T) {
	cfg := config{}
	blocks := cfg.deployPanelBlocks()
	encoded := mustJSON(t, blocks)

	if !strings.Contains(encoded, "controls - idle") {
		t.Fatalf("deploy panel should clearly say it is idle:\n%s", encoded)
	}
	if !strings.Contains(encoded, "No deploy is running") {
		t.Fatalf("deploy panel should not read like an in-progress deploy:\n%s", encoded)
	}
}

func TestDeployPanelCopiesStayIdleAndConsistent(t *testing.T) {
	want := "GoatOS deploy controls - idle"
	files := []string{
		"deploy-card.json",
		"../stg-cloudbuild-release.sh",
		"../stg-mobile-distribution.sh",
	}

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		text := string(content)
		if !strings.Contains(text, want) {
			t.Fatalf("%s does not contain canonical idle panel title %q", file, want)
		}
		if strings.Contains(text, "Deploy the current `main` branch") {
			t.Fatalf("%s still uses ambiguous non-idle deploy panel copy", file)
		}
		if strings.Contains(text, "Android build") {
			t.Fatalf("%s should say Android release, not Android build", file)
		}
	}
}

func TestMobileDistributionRequiresForceUpdateRemoteConfig(t *testing.T) {
	content, err := os.ReadFile("../stg-mobile-distribution.sh")
	if err != nil {
		t.Fatalf("read mobile distribution script: %v", err)
	}
	text := string(content)

	for _, want := range []string{
		"require_force_update_config_access",
		"FORCE_UPDATE_REMOTE_CONFIG_UPDATE_PREFLIGHT_OK",
		"validate_only=true",
		"publish_force_update_floor \"$ANDROID_VERSION_CODE\" \"https://mesha.sg/app.apk\"",
		"FORCE_UPDATE_FLOOR_PUBLISHED",
		"min_supported_version_code",
		"update_url",
		"force update blocks older builds below",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("mobile distribution script is missing %q", want)
		}
	}

	success := strings.Index(text, "notify_slack \"SUCCEEDED\"")
	publish := strings.Index(text, "publish_force_update_floor \"$ANDROID_VERSION_CODE\" \"https://mesha.sg/app.apk\"")
	if success < 0 || publish < 0 || publish > success {
		t.Fatalf("force-update floor must publish before the success notification")
	}

	preflight := strings.Index(text, "\nrequire_force_update_config_access\n")
	firebaseUpload := strings.Index(text, ":app:appDistributionUploadProdRelease")
	if preflight < 0 || firebaseUpload < 0 || preflight > firebaseUpload {
		t.Fatalf("Remote Config update preflight must run before Firebase/App APK distribution")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json marshal failed: %v", err)
	}
	return string(b)
}
