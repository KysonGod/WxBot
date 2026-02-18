package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStandaloneConfig_WithLocalOverride(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "config.json")
	localPath := filepath.Join(dir, "config.local.json")

	base := `{
  "listen_list": [{"nickname":"u1","prompt":"default"}],
  "llm": {"base_url":"https://api.deepseek.com/v1","api_key":"BASE_KEY","model":"deepseek-chat"},
  "vision": {"enabled": true},
  "vision_llm": {"base_url":"https://open.bigmodel.cn/api/paas/v4","api_key":"BASE_VISION","model":"glm-4.6v"}
}`
	local := `{
  "llm": {"api_key":"LOCAL_KEY"},
  "vision_llm": {"api_key":"LOCAL_VISION"}
}`

	if err := os.WriteFile(basePath, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadStandaloneConfig(basePath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.MainModel.APIKey != "LOCAL_KEY" {
		t.Fatalf("expected local llm key override, got %q", cfg.MainModel.APIKey)
	}
	if cfg.VisionModel.APIKey != "LOCAL_VISION" {
		t.Fatalf("expected local vision key override, got %q", cfg.VisionModel.APIKey)
	}
}

func TestResolveEnvRef(t *testing.T) {
	const key = "WXBOT_TEST_KEY"
	if err := os.Setenv(key, "ENV_VALUE"); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv(key)

	got := resolveEnvRef("env:" + key)
	if got != "ENV_VALUE" {
		t.Fatalf("expected ENV_VALUE, got %q", got)
	}
}
