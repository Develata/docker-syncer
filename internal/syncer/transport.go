package syncer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"github.com/distribution/reference"
)

// Skopeo owns the registry protocol. This adapter only reads bounded metadata
// and executes argument arrays; blobs never enter the control plane.
type Skopeo struct {
	Binary   string
	Authfile string
	Retries  int
	Timeout  time.Duration
	// Insecure is test-only/local-registry plumbing. Production callers leave
	// it false so TLS verification is always enabled.
	Insecure bool
}

type commandError struct {
	Operation string
	Code      int
	Reason    string
}

func (e *commandError) Error() string {
	return fmt.Sprintf("%s (exit %d): %s", e.Operation, e.Code, e.Reason)
}
func missing(err error) bool {
	var e *commandError
	if !errors.As(err, &e) {
		return false
	}
	s := strings.ToLower(e.Reason)
	for _, denied := range []string{"unauthorized", "authentication required", "denied", "forbidden"} {
		if strings.Contains(s, denied) {
			return false
		}
	}
	for _, absent := range []string{"manifest unknown", "manifest_unknown", "name unknown", "name_unknown", "name not known", "repository does not exist", "not found", "status code: 404", "status 404"} {
		if strings.Contains(s, absent) {
			return true
		}
	}
	return false
}
func transient(s string) bool {
	s = strings.ToLower(s)
	for _, term := range []string{"unauthorized", "authentication required", "denied", "forbidden", "invalid", "manifest unknown", "name unknown", "x509:"} {
		if strings.Contains(s, term) {
			return false
		}
	}
	for _, term := range []string{"429", "too many requests", "timeout", "connection reset", "connection refused", "temporary failure", "unexpected eof", "502", "503", "504", "tls handshake timeout"} {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}

// limitedBuffer consumes all output but retains at most limit bytes.
type limitedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remain := b.limit - b.Len()
	if n > remain {
		b.overflow = true
		p = p[:remain]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
func concise(s string) string {
	// Errors may contain registry-controlled strings. Never relay workflow commands
	// or raw control characters, and redact known credential values defensively.
	for _, key := range []string{"ALIYUN_PASSWORD", "DOCKERHUB_TOKEN", "GITHUB_TOKEN", "GHCR_TOKEN", "WEBHOOK_URL"} {
		if v := os.Getenv(key); v != "" {
			s = strings.ReplaceAll(s, v, "[redacted]")
		}
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 1500 {
		s = s[:1500] + "…"
	}
	return s
}
func (s Skopeo) command(ctx context.Context, op string, args ...string) ([]byte, error) {
	if s.Retries < 0 || s.Retries > 5 {
		return nil, fmt.Errorf("%s: retries must be between 0 and 5", op)
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	binary := s.Binary
	if binary == "" {
		binary = "skopeo"
	}
	args = append([]string{"--command-timeout", timeout.String()}, args...)
	for attempt := 0; ; attempt++ {
		out := &limitedBuffer{limit: 4 << 20}
		errout := &limitedBuffer{limit: 64 << 10}
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Stdout = out
		cmd.Stderr = errout
		err := cmd.Run()
		if err == nil {
			if out.overflow {
				return nil, fmt.Errorf("%s: metadata exceeds 4 MiB limit", op)
			}
			return out.Bytes(), nil
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s: %w", op, ctx.Err())
		}
		code := -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
		reason := concise(errout.String())
		if reason == "" {
			reason = concise(err.Error())
		}
		failure := &commandError{op, code, reason}
		if attempt >= s.Retries || !transient(reason) {
			return nil, failure
		}
		timer := time.NewTimer(time.Second * time.Duration(1<<attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("%s: %w", op, ctx.Err())
		case <-timer.C:
		}
	}
}
func (s Skopeo) inspect(ctx context.Context, ref string, config bool) ([]byte, error) {
	flag := "--raw"
	if config {
		flag = "--config"
	}
	args := []string{"inspect", "--authfile", s.Authfile}
	if s.Insecure {
		args = append(args, "--tls-verify=false")
	}
	args = append(args, flag, "docker://"+ref)
	return s.command(ctx, "inspect "+ref, args...)
}
func rawDigest(raw []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) }
func pinned(src, digest string) string {
	n, _ := reference.ParseNormalizedNamed(src)
	return n.Name() + "@" + digest
}

type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Platform  struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		Variant      string `json:"variant"`
	} `json:"platform"`
}
type manifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	MediaType     string          `json:"mediaType"`
	Manifests     []descriptor    `json:"manifests"`
	Config        json.RawMessage `json:"config"`
	Layers        json.RawMessage `json:"layers"`
}

func parseManifest(raw []byte) (manifest, error) {
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, fmt.Errorf("invalid manifest JSON: %w", err)
	}
	if m.SchemaVersion != 2 {
		return m, fmt.Errorf("unsupported manifest schema %d (schema 2 required)", m.SchemaVersion)
	}
	switch m.MediaType {
	case "application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json":
		if len(m.Manifests) == 0 {
			return m, errors.New("empty image index")
		}
	case "application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json":
		if len(m.Config) == 0 || len(m.Layers) == 0 {
			return m, errors.New("image manifest lacks config/layers")
		}
	default:
		return m, fmt.Errorf("unsupported manifest media type %q", m.MediaType)
	}
	return m, nil
}
func matches(p Platform, os, arch, variant string) bool {
	if arch == "arm64" && variant == "" {
		variant = "v8"
	}
	return p.OS == os && p.Architecture == arch && p.Variant == variant
}

// Resolve freezes the source tag before any destination is examined or changed.
// Raw index bytes identify an index; selected descriptor bytes identify a child.
// inspect's formatted .Digest and image timestamps are deliberately unused.
func (s Skopeo) Resolve(ctx context.Context, e Entry) (string, string, error) {
	raw, err := s.inspect(ctx, e.Source, false)
	if err != nil {
		return "", "", err
	}
	m, err := parseManifest(raw)
	if err != nil {
		return "", "", err
	}
	digest := rawDigest(raw)
	if n, err := reference.ParseNormalizedNamed(e.Source); err == nil {
		if d, ok := n.(reference.Digested); ok && d.Digest().String() != digest {
			return "", "", errors.New("source digest does not match raw manifest bytes")
		}
	}
	if e.Platform.All {
		return pinned(e.Source, digest), digest, nil
	}
	indexed := len(m.Manifests) > 0
	if indexed {
		var selected []descriptor
		for _, d := range m.Manifests {
			if matches(e.Platform, d.Platform.OS, d.Platform.Architecture, d.Platform.Variant) {
				selected = append(selected, d)
			}
		}
		if len(selected) != 1 {
			return "", "", fmt.Errorf("platform %s: expected exactly one descriptor, found %d", e.Platform.String(), len(selected))
		}
		digest = selected[0].Digest
		candidate := pinned(e.Source, digest)
		if _, err := ParseReference(candidate); err != nil {
			return "", "", fmt.Errorf("invalid child digest: %w", err)
		}
		raw, err = s.inspect(ctx, candidate, false)
		if err != nil {
			return "", "", err
		}
		if rawDigest(raw) != digest {
			return "", "", errors.New("child manifest digest mismatch")
		}
		m, err = parseManifest(raw)
		if err != nil {
			return "", "", err
		}
		if len(m.Manifests) > 0 {
			return "", "", errors.New("nested platform index is unsupported in single-platform mode; use all")
		}
	}
	ref := pinned(e.Source, digest)
	config, err := s.inspect(ctx, ref, true)
	if err != nil {
		return "", "", err
	}
	var p struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		Variant      string `json:"variant"`
	}
	if err := json.Unmarshal(config, &p); err != nil {
		return "", "", fmt.Errorf("invalid image config: %w", err)
	}
	// A child descriptor supplies variant when its image config omits it. For a
	// standalone ARM image with no variant evidence, fail rather than guess.
	if indexed && p.Variant == "" {
		p.Variant = e.Platform.Variant
	}
	if !matches(e.Platform, p.OS, p.Architecture, p.Variant) {
		return "", "", fmt.Errorf("image config %s/%s/%s does not match %s", p.OS, p.Architecture, p.Variant, e.Platform.String())
	}
	return ref, digest, nil
}
func (s Skopeo) Copy(ctx context.Context, src, dst string, all bool) error {
	args := []string{"copy", "--authfile", s.Authfile, "--preserve-digests", "--quiet"}
	if s.Insecure {
		args = append(args, "--src-tls-verify=false", "--dest-tls-verify=false")
	}
	if all {
		args = append(args, "--all")
	}
	args = append(args, "docker://"+src, "docker://"+dst)
	_, err := s.command(ctx, "copy "+src+" -> "+dst, args...)
	return err
}

// ReadEntries uses the same parser for CLI and batch operation. Invalid input
// is rejected during planning, before the first registry mutation.
func ReadEntries(r io.Reader, p Platform) ([]Entry, error) {
	data, err := io.ReadAll(io.LimitReader(r, 1<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, errors.New("image list exceeds 1 MiB")
	}
	var entries []Entry
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		e, err := ParseEntry(line, p)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil, errors.New("image list is empty")
	}
	return entries, nil
}
