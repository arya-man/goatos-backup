package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// Build dd97ab48-c979-43d0-95eb-3fdbaae9c20f on commit 7e0939befad2 is the regression
// this whole group exists for: Cloud Build went red on the 1800s rollout watchdog at
// 18:40:15Z, rollout r-7e0939befad2-180845-to-goatos-stg-0001 reached SUCCEEDED at
// 18:41:39Z, and the channel was told the deploy had failed before the rollout
// completed. A red build and a failed rollout are different facts.
func TestRedBuildWithSucceededRolloutIsNotReportedAsADeployFailure(t *testing.T) {
	rollout := rolloutStatus{ID: "r-7e0939befad2-180845-to-goatos-stg-0001", State: "SUCCEEDED"}
	verdict := deployVerdictFor("FAILURE", true, true, rollout)

	if strings.Contains(strings.ToLower(verdict.Title), "deploy failed") {
		t.Fatalf("a succeeded rollout must not be titled as a deploy failure: %q", verdict.Title)
	}
	if verdict.Color == colorDeployFailure {
		t.Fatalf("a succeeded rollout must not be coloured as a failure: %q", verdict.Color)
	}
	if !strings.Contains(verdict.Text, "SUCCEEDED") {
		t.Fatalf("message must say the rollout succeeded:\n%s", verdict.Text)
	}
	if !strings.Contains(verdict.Text, rollout.ID) {
		t.Fatalf("message must name the rollout it is reporting on:\n%s", verdict.Text)
	}
	for _, banned := range []string{
		"failed before backend/web rollout completed",
		"rollout did not complete",
	} {
		if strings.Contains(verdict.Text, banned) {
			t.Fatalf("message claims the rollout did not complete when it did: %q in\n%s", banned, verdict.Text)
		}
	}
	// The part that silently bites: the later steps never ran, so no Android shipped.
	if !strings.Contains(verdict.Text, "android-mobile-distribution") || !strings.Contains(verdict.Text, "stg-release-tag-bookkeeping") {
		t.Fatalf("message must name the steps that were skipped:\n%s", verdict.Text)
	}
}

func TestRedBuildWithFailedRolloutStaysALoudFailure(t *testing.T) {
	rollout := rolloutStatus{ID: "r-deadbeefcafe-101010-to-goatos-stg-0001", State: "FAILED", Description: "migrate job returned 1"}
	verdict := deployVerdictFor("FAILURE", true, false, rollout)

	if verdict.Title != "Goat OS deploy failed" {
		t.Fatalf("a genuinely failed rollout must stay a deploy failure, got %q", verdict.Title)
	}
	if verdict.Color != colorDeployFailure {
		t.Fatalf("a genuinely failed rollout must keep the failure colour, got %q", verdict.Color)
	}
	if !strings.Contains(verdict.Text, "NOT updated") {
		t.Fatalf("message must say backend/web was not updated:\n%s", verdict.Text)
	}
	if !strings.Contains(verdict.Text, "migrate job returned 1") {
		t.Fatalf("message must carry the rollout failure reason:\n%s", verdict.Text)
	}
}

func TestRedBuildWithInProgressRolloutSaysStillInProgress(t *testing.T) {
	rollout := rolloutStatus{ID: "r-deadbeefcafe-101010-to-goatos-stg-0001", State: "IN_PROGRESS"}
	verdict := deployVerdictFor("TIMED_OUT", true, false, rollout)

	if !strings.Contains(strings.ToLower(verdict.Title), "still in progress") {
		t.Fatalf("an in-progress rollout must be titled as still in progress, got %q", verdict.Title)
	}
	if strings.Contains(strings.ToLower(verdict.Title), "failed") {
		t.Fatalf("an in-progress rollout must not be titled as failed, got %q", verdict.Title)
	}
	if !strings.Contains(verdict.Text, "has NOT failed") {
		t.Fatalf("message must state the rollout has not failed:\n%s", verdict.Text)
	}
}

func TestUnreadableRolloutStateIsNeverReportedAsAFailedRollout(t *testing.T) {
	verdict := deployVerdictFor("FAILURE", true, false, rolloutStatus{})

	if !strings.Contains(verdict.Text, "UNKNOWN") {
		t.Fatalf("an unreadable rollout state must be reported as unknown:\n%s", verdict.Text)
	}
	if strings.Contains(verdict.Text, "NOT updated") {
		t.Fatalf("an unreadable rollout state must not claim backend/web was not updated:\n%s", verdict.Text)
	}
}

func TestGreenBuildStillReportsSuccess(t *testing.T) {
	verdict := deployVerdictFor("SUCCESS", true, true, rolloutStatus{})
	if verdict.Title != "Goat OS deploy succeeded" || verdict.Color != colorDeploySuccess {
		t.Fatalf("a green build must still read as a success, got %q / %q", verdict.Title, verdict.Color)
	}
}

func TestTerminalAttachmentCarriesTheRolloutStateBesideTheBuildStatus(t *testing.T) {
	rollout := rolloutStatus{ID: "r-7e0939befad2-180845-to-goatos-stg-0001", State: "SUCCEEDED"}
	steps := []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}{
		{ID: "stg-cloud-deploy-release", Status: "FAILURE"},
		{ID: "android-mobile-distribution", Status: "QUEUED"},
	}
	encoded := mustJSON(t, deployTerminalAttachments(
		"FAILURE", "<@U1>", "Backend/web deploy", "dd97ab48-c979-43d0-95eb-3fdbaae9c20f",
		true, true, "https://build", "https://deploy", steps, rollout))

	for _, want := range []string{
		"Cloud Deploy rollout",
		"SUCCEEDED (r-7e0939befad2-180845-to-goatos-stg-0001)",
		"Cloud Build status",
		"stg-cloud-deploy-release",
	} {
		if !strings.Contains(encoded, want) {
			t.Fatalf("attachment is missing %q:\n%s", want, encoded)
		}
	}
	if strings.Contains(encoded, "Goat OS deploy needs attention") {
		t.Fatalf("a succeeded rollout must not fall back to the build-status-only wording:\n%s", encoded)
	}
}

func TestAndroidOnlyJobNeverInventsARollout(t *testing.T) {
	verdict := deployVerdictFor("FAILURE", false, true, rolloutStatus{})
	if !strings.Contains(verdict.Text, "No backend/web rollout was part of this job") {
		t.Fatalf("an Android-only job must not speak about a rollout it never created:\n%s", verdict.Text)
	}
}

func TestMatchRolloutPicksTheCommitsOwnRollout(t *testing.T) {
	const prefix = "projects/goatos-stg/locations/asia-south1/deliveryPipelines/goatos-stg/releases/"
	rollouts := []cloudDeployRollout{
		{Name: prefix + "r-7eceb8285102-223003/rollouts/r-7eceb8285102-223003-to-goatos-stg-0001", State: "SUCCEEDED", CreateTime: "2026-09-21T22:33:08.000000Z"},
		{Name: prefix + "r-7e0939befad2-180845/rollouts/r-7e0939befad2-180845-to-goatos-stg-0001", State: "SUCCEEDED", CreateTime: "2026-09-22T18:10:04.537559Z"},
	}

	got := matchRollout(rollouts, "7e0939befad2")
	if got.ID != "r-7e0939befad2-180845-to-goatos-stg-0001" || got.State != "SUCCEEDED" {
		t.Fatalf("matchRollout = %+v, want the 7e0939befad2 rollout", got)
	}
	if other := matchRollout(rollouts, "000000000000"); other.State != "" {
		t.Fatalf("an unmatched commit must resolve to unknown, got %+v", other)
	}
	if blank := matchRollout(rollouts, ""); blank.State != "" {
		t.Fatalf("a blank commit must resolve to unknown, got %+v", blank)
	}
}

// Cloud Deploy refuses `orderBy=createTime desc` on this collection, so the page
// arrives in an order the API does not promise and the match must order it itself.
func TestMatchRolloutTakesTheNewestReleaseOfARedeployedCommit(t *testing.T) {
	const prefix = "projects/goatos-stg/locations/asia-south1/deliveryPipelines/goatos-stg/releases/"
	older := cloudDeployRollout{
		Name:          prefix + "r-7e0939befad2-235959/rollouts/r-7e0939befad2-235959-to-goatos-stg-0001",
		State:         "FAILED",
		CreateTime:    "2026-09-21T23:59:59.000000Z",
		FailureReason: "first attempt",
	}
	newer := cloudDeployRollout{
		Name:       prefix + "r-7e0939befad2-000101/rollouts/r-7e0939befad2-000101-to-goatos-stg-0001",
		State:      "SUCCEEDED",
		CreateTime: "2026-09-22T00:01:01.000000Z",
	}

	for _, order := range [][]cloudDeployRollout{{older, newer}, {newer, older}} {
		got := matchRollout(order, "7e0939befad2")
		if got.ID != "r-7e0939befad2-000101-to-goatos-stg-0001" || got.State != "SUCCEEDED" {
			t.Fatalf("matchRollout = %+v, want the newest rollout regardless of listing order", got)
		}
	}
}

func TestLatestRolloutForCommitFollowsCloudDeployPages(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RawQuery)
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization header = %q, want bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("pageToken") {
		case "":
			_, _ = w.Write([]byte(`{
				"rollouts": [
					{"name": "projects/goatos-stg/locations/asia-south1/deliveryPipelines/goatos-stg/releases/r-deadbeefcafe-000001/rollouts/r-deadbeefcafe-000001-to-goatos-stg-0001", "state": "FAILED", "createTime": "2026-09-22T00:00:01Z"}
				],
				"nextPageToken": "second-page"
			}`))
		case "second-page":
			_, _ = w.Write([]byte(`{
				"rollouts": [
					{"name": "projects/goatos-stg/locations/asia-south1/deliveryPipelines/goatos-stg/releases/r-7e0939befad2-180845/rollouts/r-7e0939befad2-180845-to-goatos-stg-0001", "state": "IN_PROGRESS", "createTime": "2026-09-22T18:10:04Z"}
				]
			}`))
		default:
			t.Fatalf("unexpected pageToken %q", r.URL.Query().Get("pageToken"))
		}
	}))
	defer server.Close()

	got, err := latestRolloutForCommitFromEndpoint(t.Context(), "test-token", server.URL, "7e0939befad2")
	if err != nil {
		t.Fatalf("latestRolloutForCommitFromEndpoint returned error: %v", err)
	}
	if got.ID != "r-7e0939befad2-180845-to-goatos-stg-0001" || got.State != "IN_PROGRESS" {
		t.Fatalf("rollout = %+v, want second-page matching rollout", got)
	}
	if len(requests) != 2 || !strings.Contains(requests[1], "pageToken=second-page") {
		t.Fatalf("expected two paginated requests, got %v", requests)
	}
}

func TestBuildCommitSHAResolvesTheReleaseNamePrefix(t *testing.T) {
	build := cloudBuildGetBuild{Substitutions: map[string]string{"_COMMIT_SHA": "7e0939befad2"}}
	if got := buildCommitSHA(build); got != "7e0939befad2" {
		t.Fatalf("buildCommitSHA = %q, want 7e0939befad2", got)
	}

	build = cloudBuildGetBuild{Substitutions: map[string]string{"REVISION_ID": "7e0939befad2f1c0ff33"}}
	if got := buildCommitSHA(build); got != "7e0939befad2" {
		t.Fatalf("a full revision id must shorten to the 12-char release prefix, got %q", got)
	}

	build = cloudBuildGetBuild{}
	build.SourceProvenance.ResolvedRepoSource.CommitSHA = "7e0939befad2f1c0ff33"
	if got := buildCommitSHA(build); got != "7e0939befad2" {
		t.Fatalf("source provenance must be the last resort, got %q", got)
	}

	if got := buildCommitSHA(cloudBuildGetBuild{}); got != "" {
		t.Fatalf("an unresolvable build must return no sha, got %q", got)
	}
}

func TestCloudDeployLinkUsesTheDeployRegionNotTheTriggerLocation(t *testing.T) {
	cfg := config{ProjectID: "goatos-stg", ProjectNumber: "514832198871", Location: "global", DeployRegion: "asia-south1", DeliveryPipeline: "goatos-stg"}
	url := cfg.cloudDeployURL()
	if !strings.Contains(url, "/delivery-pipelines/asia-south1/goatos-stg") {
		t.Fatalf("Cloud Deploy link must point at the deploy region: %s", url)
	}
	if strings.Contains(url, "/delivery-pipelines/global/") {
		t.Fatalf("Cloud Deploy link must not use the Cloud Build trigger location: %s", url)
	}
}

// The Slack message that actually reached the channel on 2026-09-22 was composed by
// the Cloud Build step script, not by this bot, so the shell half is pinned here too.
func TestCloudBuildReleaseScriptAsksCloudDeployBeforeAnnouncingAFailure(t *testing.T) {
	content, err := os.ReadFile("../stg-cloudbuild-release.sh")
	if err != nil {
		t.Fatalf("read stg-cloudbuild-release.sh: %v", err)
	}
	text := string(content)

	if strings.Contains(text, `notify_slack "FAILED" "Cloud Build failed before backend/web rollout completed."`) {
		t.Fatalf("the unconditional false failure notice is back; a red build does not mean the rollout did not complete")
	}
	for _, want := range []string{
		"rollout_state()",
		"gcloud deploy rollouts describe",
		"ROLLOUT_SUCCEEDED_BUILD_FAILED",
		"ROLLOUT_IN_PROGRESS",
		"ROLLOUT_UNKNOWN",
		"release_created()",
		"stg-release-tag-bookkeeping",
		"android-mobile-distribution",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("stg-cloudbuild-release.sh is missing %q", want)
		}
	}

	consult := strings.Index(text, `state="$(rollout_state)"`)
	announce := strings.Index(text, `notify_slack "ROLLOUT_SUCCEEDED_BUILD_FAILED"`)
	if consult < 0 || announce < 0 || consult > announce {
		t.Fatalf("the rollout state must be read before any failure wording is chosen")
	}
}

// A failure inside stg-clouddeploy-release.sh can happen long before "gcloud deploy
// releases create" runs - runner receipt, auth/project checks,
// dirty/non-main guards. No release exists in that case, so the build failure notice must
// say so plainly instead of reporting the rollout state as UNKNOWN.
func TestFailureBeforeReleaseCreationIsNotReportedAsUnknownRollout(t *testing.T) {
	wrapper, err := os.ReadFile("../stg-cloudbuild-release.sh")
	if err != nil {
		t.Fatalf("read stg-cloudbuild-release.sh: %v", err)
	}
	child, err := os.ReadFile("../stg-clouddeploy-release.sh")
	if err != nil {
		t.Fatalf("read stg-clouddeploy-release.sh: %v", err)
	}
	wrapperText := string(wrapper)
	childText := string(child)

	// The wrapper must never mark the release as created just because it is about to call
	// the child script; only the child, after Cloud Deploy accepts the release, may do that.
	// Anchor on the bare invocation LINE: the file also mentions the child script by name in
	// a comment, and matching that comment instead would scan only the file header and make
	// this assertion vacuous.
	const invocation = "\ntools/deploy/stg-clouddeploy-release.sh\n"
	call := strings.Index(wrapperText, invocation)
	if call < 0 {
		t.Fatalf("stg-cloudbuild-release.sh no longer invokes stg-clouddeploy-release.sh on its own line")
	}
	if strings.Contains(wrapperText[call+len(invocation):], invocation) {
		t.Fatalf("stg-clouddeploy-release.sh is invoked more than once; this assertion assumes a single call site")
	}
	if strings.Contains(wrapperText[:call], "release_requested=1") {
		t.Fatalf("the release must not be marked created before stg-clouddeploy-release.sh has created it")
	}

	// The child writes the sentinel only after the release-create command returns.
	create := strings.Index(childText, "gcloud deploy releases create")
	sentinel := strings.Index(childText, "GOATOS_STG_RELEASE_CREATED_FILE")
	if create < 0 || sentinel < 0 || sentinel < create {
		t.Fatalf("stg-clouddeploy-release.sh must write the release-created sentinel after creating the release")
	}

	// A readable rollout state must still win over a missing sentinel, so a failed sentinel
	// write can never downgrade a real in-flight rollout to "no rollout was started".
	if !strings.Contains(wrapperText, `if ! release_created && [[ -z "$state" ]]; then`) {
		t.Fatalf("the no-rollout notice must require both a missing sentinel and an unreadable rollout state")
	}
	guard := strings.Index(wrapperText, `if ! release_created && [[ -z "$state" ]]; then`)
	read := strings.Index(wrapperText, `state="$(rollout_state)"`)
	if read < 0 || read > guard {
		t.Fatalf("Cloud Deploy must be consulted before claiming no rollout was started")
	}
}

func TestRolloutWaitKeepsABoundAndIsOverridable(t *testing.T) {
	content, err := os.ReadFile("../stg-clouddeploy-release.sh")
	if err != nil {
		t.Fatalf("read stg-clouddeploy-release.sh: %v", err)
	}
	text := string(content)

	if !strings.Contains(text, `ROLLOUT_TIMEOUT_SECONDS="${GOATOS_STG_ROLLOUT_TIMEOUT_SECONDS:-3600}"`) {
		t.Fatalf("rollout wait must default to 3600s and stay overridable by GOATOS_STG_ROLLOUT_TIMEOUT_SECONDS")
	}
	if strings.Contains(text, "GOATOS_STG_ROLLOUT_TIMEOUT_SECONDS:-1800") {
		t.Fatalf("the 1800s default that failed a 1819s rollout is back")
	}
	if !strings.Contains(text, "timed out waiting for rollout") {
		t.Fatalf("the rollout wait must keep a timeout; it must not become unbounded")
	}
	if !strings.Contains(text, "last observed rollout state") {
		t.Fatalf("a rollout-wait timeout must report the state it last saw, not just that it gave up")
	}
}

// A rollout still moving through Cloud Deploy must never be offered a Deploy button.
// The notice posted beside the panel says to watch that rollout to completion, so an
// idle panel under it invites exactly the second staging deploy it warns against.
func TestIdlePanelIsWithheldWhileTheRolloutIsStillMoving(t *testing.T) {
	for _, state := range []string{"IN_PROGRESS", "PENDING", "PENDING_APPROVAL", "PENDING_RELEASE"} {
		for _, status := range []string{"FAILURE", "TIMED_OUT", "CANCELLED", "INTERNAL_ERROR"} {
			rollout := rolloutStatus{ID: "r-7e0939befad2-180845-to-goatos-stg-0001", State: state}
			if shouldPostIdlePanel(status, rollout) {
				t.Fatalf("build %s with rollout %s must not post the deploy controls", status, state)
			}
		}
	}
}

// The panel is the operator's way back in, so withholding it must stay narrow: a
// settled rollout, and a build that died before any rollout existed, both get it.
func TestIdlePanelStillReturnsOnceTheRolloutHasSettled(t *testing.T) {
	for _, state := range []string{"SUCCEEDED", "FAILED", "CANCELLED", "HALTED", ""} {
		rollout := rolloutStatus{State: state}
		if !shouldPostIdlePanel("FAILURE", rollout) {
			t.Fatalf("a failed build with rollout state %q must post the deploy controls", state)
		}
	}
	if !shouldPostIdlePanel("TIMED_OUT", rolloutStatus{}) {
		t.Fatalf("a monitor timeout with no resolvable rollout must post the deploy controls")
	}
	if shouldPostIdlePanel("SUCCESS", rolloutStatus{State: "SUCCEEDED"}) {
		t.Fatalf("a green build posts its panel elsewhere; this path must not double-post")
	}
}

// Both Go post sites and the Cloud Build trap must share the rule, or one path keeps
// handing out Deploy buttons under a live rollout.
func TestEveryTerminalPanelPostIsGatedOnTheRolloutState(t *testing.T) {
	goSrc, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(goSrc)

	if strings.Contains(text, "if build.Status != \"SUCCESS\" {\n\t\t\t\tcfg.postDeployPanel(responseURL)") {
		t.Fatalf("the terminal-build panel post is gated on build status alone again")
	}
	posts := strings.Count(text, "cfg.postDeployPanel(responseURL)")
	gated := strings.Count(text, "if shouldPostIdlePanel(")
	if posts-gated > 1 {
		t.Fatalf("found %d postDeployPanel call sites but only %d gated; every terminal post must ask shouldPostIdlePanel", posts, gated)
	}

	shell, err := os.ReadFile("../stg-cloudbuild-release.sh")
	if err != nil {
		t.Fatalf("read stg-cloudbuild-release.sh: %v", err)
	}
	shellText := string(shell)

	if strings.Contains(shellText, "notify_build_failed\n    post_deploy_panel") {
		t.Fatalf("the Cloud Build trap posts the deploy controls unconditionally again")
	}
	for _, want := range []string{
		"rollout_in_flight=1",
		`if [[ "$rollout_in_flight" != "1" ]]; then`,
	} {
		if !strings.Contains(shellText, want) {
			t.Fatalf("stg-cloudbuild-release.sh is missing %q", want)
		}
	}
	// The flag must be raised in the in-progress branch, not somewhere that would
	// withhold the panel after a settled rollout.
	inProgress := strings.Index(shellText, "IN_PROGRESS|PENDING|PENDING_APPROVAL|PENDING_RELEASE)")
	raise := strings.Index(shellText, "rollout_in_flight=1")
	failed := strings.Index(shellText, "FAILED|CANCELLED|HALTED)")
	if inProgress < 0 || raise < inProgress || (failed > 0 && raise > failed && failed > inProgress) {
		t.Fatalf("rollout_in_flight must be raised inside the in-progress branch")
	}
}
