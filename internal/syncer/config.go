package syncer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Mode            string `json:"mode"`
	Platform        string `json:"platform"`
	AliyunRegistry  string `json:"aliyun_registry"`
	AliyunNamespace string `json:"aliyun_namespace"`
	GHCRNamespace   string `json:"ghcr_namespace"`
	Retries         int    `json:"retries"`
	Timeout         string `json:"timeout"`
}

// LoadConfig applies defaults < JSON < nonempty environment. An empty path uses
// optional syncer.json; an explicitly supplied missing path is an error.
// Semantic validation is deferred so CLI flags can override configuration.
func LoadConfig(path string, lookup func(string) (string, bool)) (Config, error) {
	c := Config{Mode: "none", Platform: "linux/amd64", Retries: 2, Timeout: "10m"}
	optional := path == ""
	if optional {
		path = "syncer.json"
	}
	data, err := os.ReadFile(path)
	if err != nil && !(optional && os.IsNotExist(err)) {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err == nil {
		if err := decodeConfig(data, &c); err != nil {
			return Config{}, fmt.Errorf("decode config %s: %w", path, err)
		}
	}
	if lookup == nil {
		lookup = os.LookupEnv
	}
	fields := []struct {
		env   string
		value *string
	}{
		{"SYNC_MODE", &c.Mode}, {"SYNC_PLATFORM", &c.Platform},
		{"ALIYUN_REGISTRY", &c.AliyunRegistry}, {"ALIYUN_NAMESPACE", &c.AliyunNamespace},
		{"GHCR_NAMESPACE", &c.GHCRNamespace}, {"SYNC_TIMEOUT", &c.Timeout},
	}
	for _, f := range fields {
		if value, ok := lookup(f.env); ok && value != "" {
			*f.value = value
		}
	}
	if value, ok := lookup("SYNC_RETRIES"); ok && value != "" {
		c.Retries, err = strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("SYNC_RETRIES must be an integer: %w", err)
		}
	}
	if c.GHCRNamespace == "" {
		if owner, ok := lookup("GITHUB_REPOSITORY_OWNER"); ok {
			c.GHCRNamespace = strings.ToLower(owner)
		}
	}
	return c, nil
}

// Decode one exact, non-null object, rejecting unknown/duplicate/null fields.
func decodeConfig(data []byte, c *Config) error {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return fmt.Errorf("expected a JSON object")
	}
	fields := map[string]any{"mode": &c.Mode, "platform": &c.Platform, "aliyun_registry": &c.AliyunRegistry, "aliyun_namespace": &c.AliyunNamespace, "ghcr_namespace": &c.GHCRNamespace, "retries": &c.Retries, "timeout": &c.Timeout}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return err
		}
		key := token.(string)
		field, ok := fields[key]
		if !ok {
			return fmt.Errorf("unknown field %q", key)
		}
		if seen[key] {
			return fmt.Errorf("duplicate field %q", key)
		}
		seen[key] = true
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return err
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("null field %q", key)
		}
		if err := json.Unmarshal(raw, field); err != nil {
			return err
		}
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("unexpected content after config object")
	}
	return nil
}

func (c Config) Validate() error {
	if _, err := ParsePlatform(c.Platform); err != nil {
		return err
	}
	if c.Retries < 0 || c.Retries > 5 {
		return fmt.Errorf("retries must be between 0 and 5")
	}
	duration, err := time.ParseDuration(c.Timeout)
	if err != nil || duration <= 0 {
		return fmt.Errorf("timeout must be a positive Go duration")
	}
	_, err = c.targetPrefixes()
	return err
}

func (c Config) Targets() ([]string, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c.targetPrefixes()
}

var githubOwner = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?$`)

func (c Config) targetPrefixes() ([]string, error) {
	targets := []string{}
	switch c.Mode {
	case "none", "aliyun", "ghcr", "double":
	default:
		return nil, fmt.Errorf("invalid mode %q", c.Mode)
	}
	if c.Mode == "aliyun" || c.Mode == "double" {
		if strings.Contains(c.AliyunNamespace, "/") || c.AliyunNamespace == "" || strings.Contains(c.AliyunRegistry, "/") {
			return nil, fmt.Errorf("Aliyun requires a registry host and single-component namespace")
		}
		prefix, err := normalizePrefix(c.AliyunRegistry + "/" + c.AliyunNamespace)
		if err != nil {
			return nil, err
		}
		targets = append(targets, prefix)
	}
	if c.Mode == "ghcr" || c.Mode == "double" {
		if !githubOwner.MatchString(c.GHCRNamespace) || strings.Contains(c.GHCRNamespace, "--") {
			return nil, fmt.Errorf("invalid GHCR owner %q: expected a lowercase GitHub owner", c.GHCRNamespace)
		}
		targets = append(targets, "ghcr.io/"+c.GHCRNamespace)
	}
	return targets, nil
}
