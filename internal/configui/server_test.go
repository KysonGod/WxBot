package configui

import "testing"

func TestSetModelKeyOnlyClearsStaleFields(t *testing.T) {
	root := map[string]any{
		"llm": map[string]any{
			"provider":        "legacy-provider",
			"base_url":        "https://legacy.example/v1",
			"api_key":         "OLD_KEY",
			"model":           "legacy-model",
			"temperature":     0.8,
			"max_tokens":      999,
			"timeout_seconds": 66,
		},
	}

	setModel(root, "llm", modelUI{APIKey: "NEW_KEY"}, true)

	sectionAny, ok := root["llm"]
	if !ok {
		t.Fatalf("expected llm section to exist")
	}
	section, ok := sectionAny.(map[string]any)
	if !ok {
		t.Fatalf("llm section should be map[string]any")
	}
	if got, ok := section["api_key"]; !ok || got != "NEW_KEY" {
		t.Fatalf("expected api_key=NEW_KEY, got %#v", section["api_key"])
	}
	for _, key := range []string{"provider", "base_url", "model", "temperature", "max_tokens", "timeout_seconds"} {
		if _, exists := section[key]; exists {
			t.Fatalf("expected stale field %q to be removed", key)
		}
	}
}

func TestSetModelKeyOnlyRemovesEmptySection(t *testing.T) {
	root := map[string]any{
		"online_llm": map[string]any{
			"api_key": "OLD",
		},
	}

	setModel(root, "online_llm", modelUI{APIKey: ""}, true)

	if _, exists := root["online_llm"]; exists {
		t.Fatalf("expected online_llm section to be removed when api_key is empty")
	}
}
