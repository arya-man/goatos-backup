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

func TestSlackDeployKeepsZeroDowntimeDefaultEnabled(t *testing.T) {
	mainContent, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(mainContent), `"_GOATOS_STG_ZERO_DOWNTIME_DEPLOY": "true"`) {
		t.Fatalf("Slack-triggered backend/web deploys must force the zero-downtime Cloud Build substitution")
	}

	cloudBuildContent, err := os.ReadFile("../../../cloudbuild.stg.yaml")
	if err != nil {
		t.Fatalf("read cloudbuild.stg.yaml: %v", err)
	}
	if !strings.Contains(string(cloudBuildContent), `_GOATOS_STG_ZERO_DOWNTIME_DEPLOY: "true"`) {
		t.Fatalf("Cloud Build default must keep zero-downtime deploy enabled")
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
		"require_force_update_push_access \"$DEPLOY_VERSION_CODE\" \"https://mesha.sg/app.apk\"",
		"FORCE_UPDATE_RECHECK_PUSH_PREFLIGHT_OK",
		"validate_only=true",
		"publish_force_update_floor \"$ANDROID_VERSION_CODE\" \"https://mesha.sg/app.apk\"",
		"send_force_update_recheck_push \"$ANDROID_VERSION_CODE\" \"https://mesha.sg/app.apk\"",
		"FORCE_UPDATE_RECHECK_PUSH_SENT",
		"force_update_recheck",
		"goatos_force_update_prod",
		"https://fcm.googleapis.com/v1/projects/${PROJECT_ID}/messages:send",
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
	push := strings.Index(text, "send_force_update_recheck_push \"$ANDROID_VERSION_CODE\" \"https://mesha.sg/app.apk\"")
	if push < 0 || push > success {
		t.Fatalf("force-update recheck push must send before the success notification")
	}
	if publish > push {
		t.Fatalf("force-update floor must publish before the recheck push")
	}

	preflight := strings.Index(text, "\nrequire_force_update_config_access\n")
	firebaseUpload := strings.Index(text, ":app:appDistributionUploadProdRelease")
	if preflight < 0 || firebaseUpload < 0 || preflight > firebaseUpload {
		t.Fatalf("Remote Config update preflight must run before Firebase/App APK distribution")
	}
	pushPreflight := strings.Index(text, "require_force_update_push_access \"$DEPLOY_VERSION_CODE\" \"https://mesha.sg/app.apk\"")
	if pushPreflight < 0 || firebaseUpload < 0 || pushPreflight > firebaseUpload {
		t.Fatalf("silent force-update push preflight must run before Firebase/App APK distribution")
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
