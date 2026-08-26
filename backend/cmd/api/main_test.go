package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsSharedLocalAPIAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		addr string
		want bool
	}{
		{name: "default shared address", addr: "", want: true},
		{name: "canonical shared address", addr: "127.0.0.1:8080", want: true},
		{name: "all interfaces shared port", addr: ":8080", want: true},
		{name: "isolated e2e address", addr: "127.0.0.1:18080", want: false},
		{name: "other isolated address", addr: "127.0.0.1:28080", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isSharedLocalAPIAddress(tt.addr); got != tt.want {
				t.Fatalf("isSharedLocalAPIAddress(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestAssertLocalOriginMainStackIsolatedE2ESkipsSharedGuard(t *testing.T) {
	t.Setenv("GOATOS_ENV", "local")
	t.Setenv("GOATOS_HTTP_ADDR", "127.0.0.1:18080")
	if err := assertLocalOriginMainStack(); err != nil {
		t.Fatalf("isolated E2E API should not require shared origin/main: %v", err)
	}
}

func TestRunningInContainerHonorsKubernetesEnv(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	if !runningInContainer() {
		t.Fatal("KUBERNETES_SERVICE_HOST should identify a container runtime")
	}
}

func TestAssertLocalOriginMainStackPreverifiedSharedCheckout(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "--quiet")
	runGit(t, repo, "config", "user.email", "guard@example.invalid")
	runGit(t, repo, "config", "user.name", "Guard Test")
	tracked := filepath.Join(repo, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "tracked.txt")
	runGit(t, repo, "commit", "--quiet", "-m", "first")
	runGit(t, repo, "remote", "add", "origin", "https://github.com/vgoats/goatos.git")
	first := runGit(t, repo, "rev-parse", "HEAD")
	runGit(t, repo, "update-ref", "refs/remotes/origin/main", first)

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	t.Setenv("GOATOS_ENV", "local")
	t.Setenv("GOATOS_HTTP_ADDR", "127.0.0.1:8080")
	t.Setenv("GOATOS_ORIGIN_MAIN_PREVERIFIED", "1")
	t.Setenv("GOATOS_ALLOW_STALE_LOCAL_STACK", "0")

	if err := assertLocalOriginMainStack(); err != nil {
		t.Fatalf("clean exact-main checkout should pass: %v", err)
	}
	if err := os.WriteFile(tracked, []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := assertLocalOriginMainStack(); err == nil {
		t.Fatal("dirty shared checkout should fail")
	}
}

func runGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
