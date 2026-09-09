//go:build integration

package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type ociDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int    `json:"size"`
	Platform  *struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		Variant      string `json:"variant,omitempty"`
	} `json:"platform,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

func putBlob(t *testing.T, layout string, value any) ociDescriptor {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := fmt.Sprintf("sha256:%x", sum)
	if err := os.WriteFile(filepath.Join(layout, "blobs", "sha256", fmt.Sprintf("%x", sum)), data, 0600); err != nil {
		t.Fatal(err)
	}
	return ociDescriptor{Digest: digest, Size: len(data)}
}
func makeOCIFixture(t *testing.T) string {
	t.Helper()
	layout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(layout, "blobs", "sha256"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layout, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	platforms := []struct{ os, arch, variant string }{{"linux", "amd64", ""}, {"linux", "arm64", "v8"}, {"linux", "arm", "v7"}, {"linux", "arm", "v6"}}
	children := make([]ociDescriptor, 0, len(platforms))
	for _, p := range platforms {
		config := putBlob(t, layout, map[string]any{"architecture": p.arch, "os": p.os, "variant": p.variant, "config": map[string]any{}, "rootfs": map[string]any{"type": "layers", "diff_ids": []any{}}})
		config.MediaType = "application/vnd.oci.image.config.v1+json"
		manifest := putBlob(t, layout, map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json", "config": config, "layers": []any{}})
		manifest.MediaType = "application/vnd.oci.image.manifest.v1+json"
		manifest.Platform = &struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
			Variant      string `json:"variant,omitempty"`
		}{p.os, p.arch, p.variant}
		children = append(children, manifest)
	}
	index := putBlob(t, layout, map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": children})
	index.MediaType = "application/vnd.oci.image.index.v1+json"
	index.Annotations = map[string]string{"org.opencontainers.image.ref.name": "integration"}
	top, _ := json.Marshal(map[string]any{"schemaVersion": 2, "manifests": []ociDescriptor{index}})
	if err := os.WriteFile(filepath.Join(layout, "index.json"), top, 0600); err != nil {
		t.Fatal(err)
	}
	return layout
}

// The caller starts a disposable local distribution registry. The complete
// source image is generated as an OCI layout, so this test has no public
// registry dependency and performs no external writes.
func TestRegistryToRegistryCopy(t *testing.T) {
	registry := os.Getenv("SYNCER_INTEGRATION_REGISTRY")
	if registry == "" {
		t.Skip("set SYNCER_INTEGRATION_REGISTRY to a disposable local registry")
	}
	auth := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(auth, []byte(`{"auths":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := Skopeo{Authfile: auth, Retries: 2, Timeout: 3 * time.Minute, Insecure: true}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	layout := makeOCIFixture(t)
	source := registry + "/source/pause:3.10"
	if _, err := s.command(ctx, "seed integration fixture", "copy", "--authfile", auth, "--all", "--preserve-digests", "--dest-tls-verify=false", "oci:"+layout+":integration", "docker://"+source); err != nil {
		t.Fatal(err)
	}
	caseTag := fmt.Sprintf("i-%d", time.Now().UnixNano())
	cases := []struct{ name, platform string }{{"amd64", "linux/amd64"}, {"arm64", "linux/arm64"}, {"arm-v7", "linux/arm/v7"}, {"arm-v6", "linux/arm/v6"}, {"all", "all"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParsePlatform(tc.platform)
			if err != nil {
				t.Fatal(err)
			}
			e := Entry{Source: source, Platform: p, Target: "pause-" + tc.name + ":" + caseTag}
			plans, err := BuildPlans([]Entry{e}, []string{registry + "/docker-syncer"})
			if err != nil {
				t.Fatal(err)
			}
			first, err := Run(ctx, s, plans, RunOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(first) != 1 || first[0].Status != "SYNCED" {
				t.Fatalf("first=%+v", first)
			}
			second, err := Run(ctx, s, plans, RunOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(second) != 1 || second[0].Status != "UNCHANGED" {
				t.Fatalf("second=%+v", second)
			}
		})
	}
	t.Run("digest-pinned-double-target", func(t *testing.T) {
		p, _ := ParsePlatform("linux/amd64")
		pinnedSource, _, err := s.Resolve(ctx, Entry{Source: source, Platform: p})
		if err != nil {
			t.Fatal(err)
		}
		plans, err := BuildPlans([]Entry{{Source: pinnedSource, Platform: p, Target: "pause-digest:integration"}}, []string{registry + "/target-a", registry + "/target-b"})
		if err != nil {
			t.Fatal(err)
		}
		results, err := Run(ctx, s, plans, RunOptions{Force: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Fatalf("results=%+v", results)
		}
		for _, r := range results {
			if r.Status != "SYNCED" {
				t.Fatalf("result=%+v", r)
			}
		}
	})
}
