// Package syncer plans daemonless image copies without contacting registries.
package syncer

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/distribution/reference"
)

type Platform struct {
	OS, Architecture, Variant string
	All                       bool
}

var platformPart = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
var slugSeparators = regexp.MustCompile(`[^a-z0-9]+`)

func ParsePlatform(s string) (Platform, error) {
	if s == "all" {
		return Platform{All: true}, nil
	}
	parts := strings.Split(s, "/")
	if len(parts) < 2 || len(parts) > 3 {
		return Platform{}, fmt.Errorf("invalid platform %q: expected os/arch[/variant] or all", s)
	}
	for _, part := range parts {
		if !platformPart.MatchString(part) {
			return Platform{}, fmt.Errorf("invalid platform %q", s)
		}
	}
	if parts[0] == "all" {
		return Platform{}, fmt.Errorf("invalid platform %q: all must stand alone", s)
	}
	p := Platform{OS: parts[0], Architecture: parts[1]}
	if len(parts) == 3 {
		p.Variant = parts[2]
	}
	if p.Architecture == "arm" && p.Variant != "v6" && p.Variant != "v7" {
		return Platform{}, fmt.Errorf("arm requires explicit v6 or v7 variant")
	}
	if p.Architecture == "arm64" && p.Variant == "" {
		p.Variant = "v8"
	}
	return p, nil
}

func (p Platform) String() string {
	if p.All {
		return "all"
	}
	s := p.OS + "/" + p.Architecture
	if p.Variant != "" {
		s += "/" + p.Variant
	} else if p.Architecture == "arm64" {
		s += "/v8"
	}
	return s
}

// ParseReference canonicalizes familiar names and strips redundant digest tags.
// Importing crypto/sha256 above also registers SHA-256 for digest validation.
func ParseReference(s string) (string, error) {
	n, err := reference.ParseNormalizedNamed(strings.TrimSpace(s))
	if err != nil {
		return "", fmt.Errorf("invalid image reference %q: %w", s, err)
	}
	if d, ok := n.(reference.Digested); ok {
		if d.Digest().Algorithm().String() != "sha256" {
			return "", fmt.Errorf("only sha256 image digests are supported")
		}
		n, err = reference.WithDigest(reference.TrimNamed(n), d.Digest())
		if err != nil {
			return "", err
		}
	} else {
		n = reference.TagNameOnly(n)
	}
	domain := reference.Domain(n)
	result := strings.ToLower(domain) + strings.TrimPrefix(n.String(), domain)
	// Reparse to normalize case-insensitive Docker Hub domain aliases too.
	canonical, err := reference.ParseNormalizedNamed(result)
	if err != nil {
		return "", err
	}
	return canonical.String(), nil
}

type Entry struct {
	Source   string
	Platform Platform
	Target   string
}

func validTarget(s string) bool {
	if strings.ContainsAny(s, "/@") || strings.Count(s, ":") != 1 {
		return false
	}
	n, err := reference.ParseNormalizedNamed(s)
	if err != nil {
		return false
	}
	_, tagged := n.(reference.Tagged)
	return tagged && reference.Path(n) == "library/"+strings.SplitN(s, ":", 2)[0]
}

// ParseEntry accepts only --platform and --target, with either value syntax.
// Blank lines and full-line comments must be filtered by the caller.
func ParseEntry(line string, defaultPlatform Platform) (Entry, error) {
	e := Entry{Platform: defaultPlatform}
	seen := map[string]bool{}
	fields := strings.Fields(line)
	for i := 0; i < len(fields); i++ {
		word := fields[i]
		if strings.HasPrefix(word, "-") {
			key, value, equals := strings.Cut(word, "=")
			if key != "--platform" && key != "--target" {
				return Entry{}, fmt.Errorf("unknown option %q", key)
			}
			if seen[key] {
				return Entry{}, fmt.Errorf("duplicate option %s", key)
			}
			seen[key] = true
			if !equals {
				i++
				if i >= len(fields) {
					return Entry{}, fmt.Errorf("missing value for %s", key)
				}
				value = fields[i]
			}
			if value == "" {
				return Entry{}, fmt.Errorf("missing value for %s", key)
			}
			if key == "--platform" {
				p, err := ParsePlatform(value)
				if err != nil {
					return Entry{}, err
				}
				e.Platform = p
			} else {
				e.Target = value
			}
		} else {
			if e.Source != "" {
				return Entry{}, fmt.Errorf("expected one image per entry")
			}
			e.Source = word
		}
	}
	return normalizeEntry(e)
}

func normalizeEntry(e Entry) (Entry, error) {
	var err error
	e.Source, err = ParseReference(e.Source)
	if err != nil {
		return Entry{}, err
	}
	e.Platform, err = ParsePlatform(e.Platform.String())
	if err != nil {
		return Entry{}, err
	}
	if e.Target != "" && !validTarget(e.Target) {
		return Entry{}, fmt.Errorf("target %q must be a single lowercase repository:tag", e.Target)
	}
	return e, nil
}

// AutomaticName returns a <=50-character slug plus -- and 12 hash hex digits,
// followed by a case-preserving tag. Callers should validate entries first.
func AutomaticName(e Entry) string {
	if source, err := ParseReference(e.Source); err == nil {
		e.Source = source
	}
	hash := sha256.Sum256([]byte(e.Source + "\n" + e.Platform.String()))
	name, tag := e.Source, "latest"
	if n, err := reference.ParseNormalizedNamed(e.Source); err == nil {
		name = n.Name()
		if d, ok := n.(reference.Digested); ok {
			tag = "sha256-" + d.Digest().Encoded()
		} else if t, ok := n.(reference.Tagged); ok {
			tag = t.Tag()
		}
	}
	slug := strings.Trim(slugSeparators.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(slug) > 50 {
		slug = strings.TrimRight(slug[:50], "-")
	}
	if slug == "" {
		slug = "image"
	}
	return fmt.Sprintf("%s--%x:%s", slug, hash[:6], tag)
}

type Plan struct {
	Entry   Entry
	Targets []string
}

func normalizePrefix(s string) (string, error) {
	s = strings.TrimSuffix(strings.TrimSpace(s), "/")
	host, path, ok := strings.Cut(s, "/")
	if !ok || path == "" || strings.ContainsAny(path, ":@") || !(strings.ContainsAny(host, ".:") || strings.EqualFold(host, "localhost")) {
		return "", fmt.Errorf("invalid destination prefix %q: expected registry/namespace", s)
	}
	n, err := reference.ParseNormalizedNamed(s + "/probe:latest")
	if err != nil || !strings.EqualFold(reference.Domain(n), host) && !strings.EqualFold(host, "index.docker.io") {
		return "", fmt.Errorf("invalid destination prefix %q", s)
	}
	return strings.ToLower(reference.Domain(n)) + "/" + strings.TrimSuffix(reference.Path(n), "/probe"), nil
}

// BuildPlans normalizes and sorts all inputs before checking destination ownership.
// No partial plan is returned on error, so callers can preflight before any I/O.
func BuildPlans(entries []Entry, prefixes []string) ([]Plan, error) {
	destinations := map[string]bool{}
	for _, prefix := range prefixes {
		p, err := normalizePrefix(prefix)
		if err != nil {
			return nil, err
		}
		destinations[p] = true
	}
	ordered := make([]string, 0, len(destinations))
	for p := range destinations {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)
	normalized := make([]Entry, 0, len(entries))
	sources := map[string]bool{}
	for _, entry := range entries {
		e, err := normalizeEntry(entry)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, e)
		sources[e.Source] = true
	}
	sort.Slice(normalized, func(i, j int) bool {
		return entryKey(normalized[i])+"\n"+normalized[i].Target < entryKey(normalized[j])+"\n"+normalized[j].Target
	})
	owners, seen := map[string]string{}, map[string]bool{}
	plans := make([]Plan, 0, len(normalized))
	for _, e := range normalized {
		name := e.Target
		if name == "" {
			name = AutomaticName(e)
		}
		targets := make([]string, 0, len(ordered))
		for _, prefix := range ordered {
			target, err := ParseReference(prefix + "/" + name)
			if err != nil {
				return nil, err
			}
			if sources[target] {
				return nil, fmt.Errorf("destination %s is also a source", target)
			}
			if owner, exists := owners[target]; exists && owner != entryKey(e) {
				return nil, fmt.Errorf("destination collision at %s", target)
			}
			owners[target] = entryKey(e)
			targets = append(targets, target)
		}
		key := entryKey(e) + "\n" + strings.Join(targets, "\n")
		if !seen[key] {
			plans = append(plans, Plan{Entry: e, Targets: targets})
			seen[key] = true
		}
	}
	return plans, nil
}

func entryKey(e Entry) string { return e.Source + "\n" + e.Platform.String() }
