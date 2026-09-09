package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDryRunExercisesProductionPlannerWithoutSkopeo(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "syncer.json")
	if err := os.WriteFile(config, []byte(`{"mode":"ghcr","ghcr_namespace":"develata","platform":"linux/arm/v7","retries":2,"timeout":"10m"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	err := run(context.Background(), []string{"sync", "--config", config, "--image", "registry.example.com:5000/path/image:v2", "--dry-run"}, &out, &errout)
	if err != nil {
		t.Fatalf("run: %v; stderr=%s", err, errout.String())
	}
	text := out.String()
	for _, want := range []string{"registry.example.com:5000/path/image:v2", "linux/arm/v7", "ghcr.io/develata/", "[SKIPPED]", "FAILED=0"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
}
func TestCLIOverridesEnvironmentAndConfig(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "syncer.json")
	if err := os.WriteFile(config, []byte(`{"mode":"aliyun","aliyun_registry":"registry.example.com","aliyun_namespace":"n","platform":"linux/amd64","retries":2,"timeout":"10m"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SYNC_MODE", "none")
	t.Setenv("GHCR_NAMESPACE", "")
	t.Setenv("GITHUB_REPOSITORY_OWNER", "")
	var out, errout bytes.Buffer
	if err := run(context.Background(), []string{"config", "--config", config, "--mode", "ghcr"}, &out, &errout); err == nil {
		t.Fatal("missing GHCR owner should still fail validation")
	}
	t.Setenv("GHCR_NAMESPACE", "develata")
	out.Reset()
	errout.Reset()
	if err := run(context.Background(), []string{"config", "--config", config, "--mode", "ghcr"}, &out, &errout); err != nil {
		t.Fatal(err)
	}
	if out.String() != "ghcr\n" {
		t.Fatalf("got %q", out.String())
	}
}
