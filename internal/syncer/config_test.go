package syncer

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func envMap(m map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) { v, ok := m[key]; return v, ok }
}
func configFile(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "syncer.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func baseConfig() Config {
	return Config{Mode: "none", Platform: "linux/amd64", Retries: 2, Timeout: "10m"}
}

func TestConfigDefaultsAndOptionalFile(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Error(err)
		}
	})
	got, err := LoadConfig("", envMap(nil))
	if err != nil || got != baseConfig() {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := LoadConfig("syncer.json", envMap(nil)); err == nil {
		t.Fatal("explicit missing file accepted")
	}
	if err := os.WriteFile("syncer.json", []byte(`{"mode":"ghcr","ghcr_namespace":"owner"}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = LoadConfig("", envMap(nil))
	if err != nil || got.Mode != "ghcr" {
		t.Fatalf("default file not loaded: %+v %v", got, err)
	}
}

func TestConfigPrecedence(t *testing.T) {
	path := configFile(t, `{"mode":"aliyun","platform":"linux/arm/v7","aliyun_registry":"registry.example:5000","aliyun_namespace":"file-ns","ghcr_namespace":"file-owner","retries":4,"timeout":"3m"}`)
	got, err := LoadConfig(path, envMap(map[string]string{
		"SYNC_MODE": "double", "SYNC_PLATFORM": "all", "ALIYUN_REGISTRY": "registry.other",
		"ALIYUN_NAMESPACE": "env-ns", "GHCR_NAMESPACE": "env-owner", "SYNC_RETRIES": "0", "SYNC_TIMEOUT": "5s", "GITHUB_REPOSITORY_OWNER": "Fallback",
	}))
	want := Config{Mode: "double", Platform: "all", AliyunRegistry: "registry.other", AliyunNamespace: "env-ns", GHCRNamespace: "env-owner", Retries: 0, Timeout: "5s"}
	if err != nil || got != want {
		t.Fatalf("got %+v %v; want %+v", got, err, want)
	}
	targets, err := got.Targets()
	if err != nil || !reflect.DeepEqual(targets, []string{"registry.other/env-ns", "ghcr.io/env-owner"}) {
		t.Fatalf("targets %v %v", targets, err)
	}
	got, err = LoadConfig(path, envMap(map[string]string{"SYNC_MODE": "", "SYNC_RETRIES": "", "SYNC_TIMEOUT": "", "GHCR_NAMESPACE": "", "GITHUB_REPOSITORY_OWNER": "Ignored"}))
	if err != nil || got.Mode != "aliyun" || got.Retries != 4 || got.Timeout != "3m" || got.GHCRNamespace != "file-owner" {
		t.Fatalf("empty env overrides file: %+v %v", got, err)
	}
	got, err = LoadConfig(configFile(t, `{}`), envMap(map[string]string{"GITHUB_REPOSITORY_OWNER": "Mixed-Owner"}))
	if err != nil || got.GHCRNamespace != "mixed-owner" {
		t.Fatalf("fallback failed: %+v %v", got, err)
	}
}

func TestConfigRejectsMalformedInput(t *testing.T) {
	for _, data := range []string{``, `null`, `[]`, `{"unknown":1}`, `{"Mode":"none"}`, `{"mode":null}`, `{"retries":"2"}`, `{"retries":2.5}`, `{"mode":"none","mode":"ghcr"}`, `{} {}`, `{} trailing`, `{"mode":`, `{"mode":true}`, `{"retries":999999999999999999999999999999}`} {
		t.Run(data, func(t *testing.T) {
			if _, err := LoadConfig(configFile(t, data), envMap(nil)); err == nil {
				t.Fatalf("accepted %q", data)
			}
		})
	}
	for _, value := range []string{"two", "1.5", " 2", "999999999999999999999999"} {
		if _, err := LoadConfig(configFile(t, `{}`), envMap(map[string]string{"SYNC_RETRIES": value})); err == nil {
			t.Fatalf("accepted env retries %q", value)
		}
	}
	if _, err := LoadConfig(t.TempDir(), envMap(nil)); err == nil {
		t.Fatal("accepted directory as config")
	}
}

func TestConfigDefersSemanticValidation(t *testing.T) {
	got, err := LoadConfig(configFile(t, `{"mode":"bad","platform":"bad","retries":-1,"timeout":"bad"}`), envMap(nil))
	if err != nil {
		t.Fatalf("validation prevented CLI overrides: %v", err)
	}
	if err := got.Validate(); err == nil {
		t.Fatal("invalid loaded config validates")
	}
	got.Mode = "none"
	got.Platform = "all"
	got.Retries = 0
	got.Timeout = "1s"
	if err := got.Validate(); err != nil {
		t.Fatalf("CLI overrides not effective: %v", err)
	}
	got, err = LoadConfig(configFile(t, `{}`), envMap(map[string]string{"SYNC_TIMEOUT": "nonsense", "SYNC_PLATFORM": "wrong", "SYNC_MODE": "bad", "SYNC_RETRIES": "-1"}))
	if err != nil {
		t.Fatal("semantic env validation was not deferred:", err)
	}
	if err := got.Validate(); err == nil {
		t.Fatal("invalid env validates")
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"mode", func(c *Config) { c.Mode = "typo" }}, {"platform", func(c *Config) { c.Platform = "linux/arm" }},
		{"negative retries", func(c *Config) { c.Retries = -1 }}, {"excessive retries", func(c *Config) { c.Retries = 6 }}, {"zero timeout", func(c *Config) { c.Timeout = "0s" }},
		{"negative timeout", func(c *Config) { c.Timeout = "-1s" }}, {"bad timeout", func(c *Config) { c.Timeout = "10" }},
		{"missing aliyun", func(c *Config) { c.Mode = "aliyun" }},
		{"aliyun path host", func(c *Config) {
			c.Mode = "aliyun"
			c.AliyunRegistry = "registry.example/path"
			c.AliyunNamespace = "ns"
		}},
		{"aliyun nested namespace", func(c *Config) {
			c.Mode = "aliyun"
			c.AliyunRegistry = "registry.example"
			c.AliyunNamespace = "ns/nested"
		}},
		{"aliyun namespace tag", func(c *Config) {
			c.Mode = "aliyun"
			c.AliyunRegistry = "registry.example"
			c.AliyunNamespace = "ns:tag"
		}},
		{"missing ghcr", func(c *Config) { c.Mode = "ghcr" }},
		{"uppercase ghcr", func(c *Config) { c.Mode = "ghcr"; c.GHCRNamespace = "Owner" }},
		{"ghcr path", func(c *Config) { c.Mode = "ghcr"; c.GHCRNamespace = "owner/path" }},
		{"ghcr consecutive hyphens", func(c *Config) { c.Mode = "ghcr"; c.GHCRNamespace = "owner--name" }},
		{"ghcr long owner", func(c *Config) { c.Mode = "ghcr"; c.GHCRNamespace = strings.Repeat("a", 40) }},
		{"incomplete double", func(c *Config) { c.Mode = "double"; c.GHCRNamespace = "owner" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := baseConfig()
			tc.edit(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("Validate accepted invalid config")
			}
			if targets, err := c.Targets(); err == nil || targets != nil {
				t.Fatalf("Targets returned %v %v", targets, err)
			}
		})
	}
	for _, tc := range []struct {
		mode string
		want []string
	}{
		{"none", []string{}}, {"aliyun", []string{"registry.example:5000/ns"}}, {"ghcr", []string{"ghcr.io/owner"}}, {"double", []string{"registry.example:5000/ns", "ghcr.io/owner"}},
	} {
		c := baseConfig()
		c.Mode = tc.mode
		c.AliyunRegistry = "registry.example:5000"
		c.AliyunNamespace = "ns"
		c.GHCRNamespace = "owner"
		got, err := c.Targets()
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v %v", tc.mode, got, err)
		}
	}
	// Inactive target settings do not block none mode or CLI-driven mode changes.
	c := baseConfig()
	c.GHCRNamespace = "INVALID"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
