package syncer

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const testDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestParsePlatform(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"all", "all"}, {"linux/amd64", "linux/amd64"}, {"linux/arm/v6", "linux/arm/v6"}, {"linux/arm/v7", "linux/arm/v7"},
		{"linux/arm64", "linux/arm64/v8"}, {"linux/arm64/v8", "linux/arm64/v8"}, {"linux/amd64/v3", "linux/amd64/v3"}, {"freebsd/riscv64", "freebsd/riscv64"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			p, err := ParsePlatform(tc.input)
			if err != nil || p.String() != tc.want {
				t.Fatalf("got %v, %v; want %s", p, err, tc.want)
			}
			again, err := ParsePlatform(p.String())
			if err != nil || again != p {
				t.Fatal("platform not idempotent")
			}
		})
	}
	for _, input := range []string{"", "linux", "linux/", "/amd64", "linux/arm", "linux/arm/v8", "linux/arm/7", "Linux/amd64", "linux/ARM64", "linux/amd64/", "linux/arm/v7/extra", "all/amd64", "linux/amd64;echo", " linux/amd64", "linux/amd64/-v3"} {
		t.Run("invalid-"+input, func(t *testing.T) {
			if _, err := ParsePlatform(input); err == nil {
				t.Fatalf("accepted %q", input)
			}
		})
	}
}

func TestParseReference(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"nginx", "docker.io/library/nginx:latest"}, {"library/nginx:RC1", "docker.io/library/nginx:RC1"},
		{"docker.io/nginx", "docker.io/library/nginx:latest"}, {"index.docker.io/library/nginx:latest", "docker.io/library/nginx:latest"},
		{"DOCKER.IO/nginx:RC1", "docker.io/library/nginx:RC1"}, {"ghcr.io/owner/image:RC1", "ghcr.io/owner/image:RC1"},
		{"localhost:5000/a/b", "localhost:5000/a/b:latest"}, {"registry.example:5443/image:Mixed", "registry.example:5443/image:Mixed"},
		{"nginx@sha256:" + testDigest, "docker.io/library/nginx@sha256:" + testDigest},
		{"nginx:ignored@sha256:" + testDigest, "docker.io/library/nginx@sha256:" + testDigest},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseReference(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("got %q %v; want %q", got, err, tc.want)
			}
			again, err := ParseReference(got)
			if err != nil || again != got {
				t.Fatal("reference not idempotent")
			}
		})
	}
	for _, input := range []string{"", "https://docker.io/nginx", "Foo/Bar", "nginx:", "nginx@sha256:bad", "nginx@sha512:" + strings.Repeat("a", 128), "nginx@sha256:" + strings.Repeat("A", 64), "nginx #comment", "-nginx", "nginx;touch"} {
		t.Run("invalid-"+input, func(t *testing.T) {
			if _, err := ParseReference(input); err == nil {
				t.Fatalf("accepted %q", input)
			}
		})
	}
}

func mustPlatform(t *testing.T, s string) Platform {
	t.Helper()
	p, err := ParsePlatform(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func mustEntry(t *testing.T, s string) Entry {
	t.Helper()
	e, err := ParseEntry(s, mustPlatform(t, "linux/amd64"))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestParseEntry(t *testing.T) {
	want := Entry{Source: "docker.io/library/nginx:RC1", Platform: mustPlatform(t, "linux/arm64"), Target: "mirror:Release"}
	for _, input := range []string{
		"nginx:RC1 --platform=linux/arm64 --target=mirror:Release",
		"--target mirror:Release nginx:RC1 --platform linux/arm64/v8",
		"--platform linux/arm64 --target=mirror:Release nginx:RC1\r",
	} {
		got, err := ParseEntry(input, mustPlatform(t, "linux/amd64"))
		if err != nil || got != want {
			t.Errorf("%s: got %+v %v", input, got, err)
		}
	}
	for _, input := range []string{"", "#comment", "nginx #comment", "nginx redis", "--platform linux/amd64", "nginx --platform", "nginx --platform=", "nginx --platform=linux/amd64 --platform=all", "nginx --pull-always", "nginx --target", "nginx --target=x:1 --target=y:1", "nginx --target=owner/image:1", "nginx --target=Upper:1", "nginx --target=image", "nginx --target=image@sha256:" + testDigest, "nginx --target='image:1'", "nginx --platform --target=image:1"} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseEntry(input, mustPlatform(t, "linux/amd64")); err == nil {
				t.Fatalf("accepted %q", input)
			}
		})
	}
	if _, err := ParseEntry("nginx", Platform{}); err == nil {
		t.Fatal("accepted missing default platform")
	}
	if _, err := ParseEntry("nginx --platform all", Platform{}); err != nil {
		t.Fatal("valid override should replace invalid default:", err)
	}
}

func TestAutomaticName(t *testing.T) {
	e := mustEntry(t, "nginx:RC1")
	h := sha256.Sum256([]byte(e.Source + "\n" + e.Platform.String()))
	if got, want := AutomaticName(e), fmt.Sprintf("docker-io-library-nginx--%x:RC1", h[:6]); got != want {
		t.Fatalf("got %s; want %s", got, want)
	}
	if AutomaticName(e) != AutomaticName(Entry{Source: "nginx:RC1", Platform: e.Platform}) {
		t.Fatal("source aliases produce different names")
	}
	a, b := mustEntry(t, "foo/bar_baz"), mustEntry(t, "foo_bar/baz")
	if AutomaticName(a) == AutomaticName(b) {
		t.Fatal("flattening collision")
	}
	b = a
	b.Platform = mustPlatform(t, "linux/arm64")
	if AutomaticName(a) == AutomaticName(b) {
		t.Fatal("platform collision")
	}
	b.Platform = mustPlatform(t, "linux/arm64/v8")
	c := b
	c.Platform.Variant = ""
	if AutomaticName(b) != AutomaticName(c) {
		t.Fatal("arm64 aliases differ")
	}
	pinned := mustEntry(t, "nginx:ignored@sha256:"+testDigest)
	if !strings.HasSuffix(AutomaticName(pinned), ":sha256-"+testDigest) {
		t.Fatal("digest tag lost")
	}
	long := mustEntry(t, "example.com/"+strings.Repeat("a", 100)+":TAG")
	repo, _, _ := strings.Cut(AutomaticName(long), ":")
	if len(repo) > 64 || !validTarget(AutomaticName(long)) {
		t.Fatalf("invalid long name %s", repo)
	}
}

func TestBuildPlans(t *testing.T) {
	a, b := mustEntry(t, "nginx"), mustEntry(t, "redis --platform=all")
	alias := a
	alias.Source = "index.docker.io/library/nginx:latest"
	prefixes := []string{"ghcr.io/owner/", "registry.example:5000/ns", "ghcr.io/owner"}
	got, err := BuildPlans([]Entry{b, a, alias}, prefixes)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[0].Targets) != 2 {
		t.Fatalf("dedupe failed: %+v", got)
	}
	other, err := BuildPlans([]Entry{a, b}, []string{"registry.example:5000/ns", "ghcr.io/owner"})
	if err != nil || !reflect.DeepEqual(got, other) {
		t.Fatalf("nondeterministic: %+v %v", other, err)
	}
	if got[0].Entry.Source != a.Source || !strings.HasPrefix(got[0].Targets[0], "ghcr.io/") {
		t.Fatal("plans not sorted")
	}
	original := []Entry{{Source: "nginx", Platform: a.Platform}}
	_, err = BuildPlans(original, prefixes)
	if err != nil || original[0].Source != "nginx" {
		t.Fatal("mutated caller inputs")
	}
	noTargets, err := BuildPlans([]Entry{a}, nil)
	if err != nil || len(noTargets) != 1 || len(noTargets[0].Targets) != 0 {
		t.Fatal("none mode plan failed")
	}
}

func TestBuildPlansRejectsCollisions(t *testing.T) {
	for _, lines := range [][]string{
		{"nginx --target=shared:1", "redis --target=shared:1"},
		{"nginx --target=shared:1", "nginx --platform=all --target=shared:1"},
		{"ghcr.io/owner/shared:1 --target=shared:1"},
		{"ghcr.io/owner/shared:1", "nginx --target=shared:1"},
	} {
		entries := []Entry{}
		for _, line := range lines {
			entries = append(entries, mustEntry(t, line))
		}
		plans, err := BuildPlans(entries, []string{"ghcr.io/owner"})
		if err == nil || plans != nil {
			t.Errorf("accepted collision %v", lines)
		}
	}
	e := mustEntry(t, "nginx")
	explicit := e
	explicit.Target = AutomaticName(e)
	plans, err := BuildPlans([]Entry{e, explicit}, []string{"ghcr.io/owner"})
	if err != nil || len(plans) != 1 {
		t.Fatalf("identical effective plan not deduplicated: %v %v", plans, err)
	}
	for _, prefix := range []string{"", "ghcr.io", "owner/ns", "https://ghcr.io/owner", "ghcr.io/Owner", "ghcr.io/owner:tag", "ghcr.io/../owner"} {
		if _, err := BuildPlans([]Entry{e}, []string{prefix}); err == nil {
			t.Errorf("accepted prefix %q", prefix)
		}
	}
	for _, e := range []Entry{{Source: "invalid;", Platform: mustPlatform(t, "all")}, {Source: "nginx"}, {Source: "nginx", Platform: mustPlatform(t, "all"), Target: "bad"}} {
		if _, err := BuildPlans([]Entry{e}, nil); err == nil {
			t.Errorf("accepted invalid entry %+v", e)
		}
	}
}
