package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func digestOf(b []byte) string { h := sha256.Sum256(b); return fmt.Sprintf("sha256:%x", h) }
func fakeSkopeo(t *testing.T, files map[string][]byte) (Skopeo, string) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	script := `#!/bin/sh
set -eu
raw=false
config=false
previous=
last=
for arg do
  [ "$arg" = "--raw" ] && raw=true
  [ "$arg" = "--config" ] && config=true
  previous=$last
  last=$arg
done
case "$*" in
  *" copy "*) exit "${FAKE_COPY_EXIT:-0}" ;;
esac
case "$last" in
  docker://docker.io/library/demo:latest) file=index ;;
  docker://docker.io/library/demo@sha256:*) file=child ;;
  docker://docker.io/library/standalone:latest|docker://docker.io/library/standalone@sha256:*) file=standalone ;;
  docker://example.com/ns/standalone:latest) file=standalone ;;
  docker://missing.invalid/*) echo 'manifest unknown' >&2; exit 1 ;;
  *) echo 'unauthorized: test rejection' >&2; exit 1 ;;
esac
if $config; then file="${file}-config"; fi
exec /bin/cat "$FAKE_FIXTURES/$file"
`
	path := filepath.Join(dir, "skopeo")
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	auth := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(auth, []byte(`{"auths":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_FIXTURES", dir)
	return Skopeo{Binary: path, Authfile: auth, Timeout: time.Second}, auth
}
func TestResolveSelectsExactVariantDescriptor(t *testing.T) {
	child := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{},"layers":[]}`)
	childDigest := digestOf(child)
	indexObject := map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": []any{
		map[string]any{"mediaType": "application/vnd.oci.image.manifest.v1+json", "digest": "sha256:" + strings.Repeat("a", 64), "platform": map[string]string{"os": "linux", "architecture": "arm", "variant": "v6"}},
		map[string]any{"mediaType": "application/vnd.oci.image.manifest.v1+json", "digest": childDigest, "platform": map[string]string{"os": "linux", "architecture": "arm", "variant": "v7"}}}}
	index, _ := json.Marshal(indexObject)
	s, _ := fakeSkopeo(t, map[string][]byte{"index": index, "child": child, "child-config": []byte(`{"os":"linux","architecture":"arm"}`), "standalone": child, "standalone-config": []byte(`{"os":"linux","architecture":"amd64"}`)})
	e := Entry{Source: "docker.io/library/demo:latest", Platform: Platform{OS: "linux", Architecture: "arm", Variant: "v7"}}
	source, digest, err := s.Resolve(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if digest != childDigest || source != "docker.io/library/demo@"+childDigest {
		t.Fatalf("got source=%s digest=%s", source, digest)
	}
}
func TestResolveAllUsesIndexDigest(t *testing.T) {
	child := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{},"layers":[]}`)
	index := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:` + strings.Repeat("a", 64) + `","platform":{"os":"linux","architecture":"amd64"}}]}`)
	s, _ := fakeSkopeo(t, map[string][]byte{"index": index, "child": child, "child-config": []byte(`{"os":"linux","architecture":"amd64"}`), "standalone": child, "standalone-config": []byte(`{"os":"linux","architecture":"amd64"}`)})
	source, digest, err := s.Resolve(context.Background(), Entry{Source: "docker.io/library/demo:latest", Platform: Platform{All: true}})
	if err != nil {
		t.Fatal(err)
	}
	if digest != digestOf(index) || source != "docker.io/library/demo@"+digest {
		t.Fatalf("got source=%s digest=%s", source, digest)
	}
}
func TestResolveStandaloneChecksImageConfig(t *testing.T) {
	image := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{},"layers":[]}`)
	s, _ := fakeSkopeo(t, map[string][]byte{"standalone": image, "standalone-config": []byte(`{"os":"linux","architecture":"amd64"}`), "index": image, "child": image, "child-config": []byte(`{}`)})
	_, _, err := s.Resolve(context.Background(), Entry{Source: "docker.io/library/standalone:latest", Platform: Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected platform mismatch, got %v", err)
	}
}
func TestRunAggregatesAndReturnsFailure(t *testing.T) {
	// Resolution failures are repeated into every target result; none are omitted.
	s, _ := fakeSkopeo(t, map[string][]byte{})
	plans := []Plan{{Entry: Entry{Source: "missing.invalid/a:latest", Platform: Platform{OS: "linux", Architecture: "amd64"}}, Targets: []string{"example.com/ns/a:latest", "example.com/ns/b:latest"}}}
	results, err := Run(context.Background(), s, plans, RunOptions{})
	if err == nil || len(results) != 2 {
		t.Fatalf("results=%d err=%v", len(results), err)
	}
	for _, r := range results {
		if r.Status != "FAILED" || !strings.Contains(r.Reason, "manifest unknown") {
			t.Fatalf("bad result: %+v", r)
		}
	}
}
func TestRunPreservesSuccessWhenAnotherImageFails(t *testing.T) {
	image := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{},"layers":[]}`)
	s, _ := fakeSkopeo(t, map[string][]byte{"standalone": image, "standalone-config": []byte(`{"os":"linux","architecture":"amd64"}`)})
	platform := Platform{OS: "linux", Architecture: "amd64"}
	plans := []Plan{
		{Entry: Entry{Source: "docker.io/library/standalone:latest", Platform: platform}, Targets: []string{"example.com/ns/standalone:latest"}},
		{Entry: Entry{Source: "missing.invalid/a:latest", Platform: platform}, Targets: []string{"example.com/ns/missing:latest"}},
	}
	results, err := Run(context.Background(), s, plans, RunOptions{})
	if err == nil || len(results) != 2 || results[0].Status != "UNCHANGED" || results[1].Status != "FAILED" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestAuthenticationFailureIsHardFailure(t *testing.T) {
	s, _ := fakeSkopeo(t, map[string][]byte{})
	plan := Plan{Entry: Entry{Source: "private.invalid/a:latest", Platform: Platform{OS: "linux", Architecture: "amd64"}}, Targets: []string{"example.com/ns/a:latest"}}
	results, err := Run(context.Background(), s, []Plan{plan}, RunOptions{})
	if err == nil || len(results) != 1 || results[0].Status != "FAILED" || !strings.Contains(results[0].Reason, "unauthorized") {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}
func TestDryRunNeverExecutesSkopeo(t *testing.T) {
	plans := []Plan{{Entry: Entry{Source: "docker.io/library/demo:latest", Platform: Platform{All: true}}, Targets: []string{"example.com/ns/demo:latest"}}}
	results, err := Run(context.Background(), Skopeo{Binary: "/definitely/not/present"}, plans, RunOptions{DryRun: true})
	if err != nil || len(results) != 1 || results[0].Status != "SKIPPED" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestFailureClassificationAndRedaction(t *testing.T) {
	for _, text := range []string{"manifest unknown", "repository name not known to registry", "404 Not Found"} {
		if !missing(&commandError{Reason: text}) {
			t.Errorf("absent target not recognized: %q", text)
		}
	}
	for _, text := range []string{"404 unauthorized", "forbidden: repository not found"} {
		if missing(&commandError{Reason: text}) {
			t.Errorf("credential failure treated as absent: %q", text)
		}
	}
	if _, err := (Skopeo{Retries: 6}).command(context.Background(), "test", "inspect"); err == nil {
		t.Fatal("unbounded retries accepted")
	}
	for _, deterministic := range []string{"unauthorized", "manifest unknown", "x509: unknown authority", "invalid reference"} {
		if transient(deterministic) {
			t.Errorf("deterministic failure classified transient: %q", deterministic)
		}
	}
	for _, temporary := range []string{"429 Too Many Requests", "connection reset by peer", "503 Service Unavailable", "unexpected EOF"} {
		if !transient(temporary) {
			t.Errorf("temporary failure not classified transient: %q", temporary)
		}
	}
	t.Setenv("GITHUB_TOKEN", "super-secret-token")
	got := concise("registry said\n::error:: super-secret-token")
	if strings.Contains(got, "super-secret-token") || strings.Contains(got, "\n") {
		t.Fatalf("unsafe diagnostic: %q", got)
	}
}
