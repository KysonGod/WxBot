package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"wxbot/internal/shared/config"
)

const (
	configModeFile = "file"
	configModeCLI  = "cli"
	configModeUI   = "ui"
)

type modelInput struct {
	Provider string
	BaseURL  string
	APIKey   string
	Model    string
}

func chooseConfigSourceMode(mode string, interactive bool, in io.Reader, out io.Writer) (string, error) {
	_ = interactive
	_ = in
	_ = out
	normalized := strings.ToLower(strings.TrimSpace(mode))
	switch normalized {
	case "", "auto":
		return configModeFile, nil
	case configModeFile, configModeCLI, configModeUI:
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid -config-mode: %s (allowed: auto|file|cli|ui)", mode)
	}
}

func resolveBotConfigPath(baseDir, botConfigPath string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	base := strings.TrimSpace(baseDir)
	if base == "" {
		base = wd
	}
	if !filepath.IsAbs(base) {
		base = filepath.Join(wd, base)
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return "", err
	}

	cfg := strings.TrimSpace(botConfigPath)
	if cfg == "" {
		cfg = filepath.Join(base, "config.json")
	} else if !filepath.IsAbs(cfg) {
		cfg = filepath.Join(base, cfg)
	}
	cfg, err = filepath.Abs(cfg)
	if err != nil {
		return "", err
	}
	return cfg, nil
}

func isInteractiveTerminal() bool {
	inStat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	outStat, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (inStat.Mode()&os.ModeCharDevice) != 0 && (outStat.Mode()&os.ModeCharDevice) != 0
}

func runModelSetupWizard(botConfigPath string, in io.Reader, out io.Writer, logger *log.Logger) error {
	baseCfg, err := config.LoadLocalRuntimeConfig(botConfigPath)
	if err != nil {
		return fmt.Errorf("read base config failed: %w", err)
	}
	localPath, err := config.ResolveLocalOverridePath(botConfigPath)
	if err != nil {
		return fmt.Errorf("resolve local override path failed: %w", err)
	}

	localCfg := config.LocalRuntimeConfig{}
	if _, err := os.Stat(localPath); err == nil {
		localCfg, err = config.LoadLocalRuntimeConfig(localPath)
		if err != nil {
			return fmt.Errorf("read local override failed: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check local override failed: %w", err)
	}

	localMap, err := readJSONMap(localPath)
	if err != nil {
		return err
	}

	mainCurrent := mergeModelOverride(baseCfg.LLM, localCfg.LLM)
	assistantCurrent := mergeModelOverride(baseCfg.AssistantLLM, localCfg.AssistantLLM)
	visionCurrent := mergeModelOverride(baseCfg.VisionLLM, localCfg.VisionLLM)
	onlineCurrent := mergeModelOverride(baseCfg.OnlineLLM, localCfg.OnlineLLM)

	reader := bufio.NewReader(in)
	_, _ = fmt.Fprintln(out, "")
	_, _ = fmt.Fprintln(out, "================ 模型配置向导 ================")
	_, _ = fmt.Fprintf(out, "主配置文件: %s\n", botConfigPath)
	_, _ = fmt.Fprintf(out, "本地覆盖文件: %s\n", localPath)
	_, _ = fmt.Fprintln(out, "说明：主模型必须配置；其他模型可跳过。跳过后对应功能不可用，若要启用需重启应用后重新配置。")

	mainModel, err := promptRequiredModelStep(reader, out, 1, "主模型（核心聊天，必填）", mainCurrent)
	if err != nil {
		return err
	}

	assistantModel, assistantConfigured, err := promptOptionalModelStep(
		reader, out, 2,
		"助手模型（可选，独立助手模型）",
		assistantCurrent,
		"跳过后将不使用独立助手模型，回复链路会复用主模型；如需独立助手模型请重启后重配。",
	)
	if err != nil {
		return err
	}

	visionModel, visionConfigured, err := promptOptionalModelStep(
		reader, out, 3,
		"视觉模型（可选，图片/表情识别）",
		visionCurrent,
		"跳过后图片/表情识别功能不可用；如需启用请重启后重配。",
	)
	if err != nil {
		return err
	}

	onlineModel, onlineConfigured, err := promptOptionalModelStep(
		reader, out, 4,
		"联网模型（可选，联网检索）",
		onlineCurrent,
		"跳过后联网检索功能不可用；如需启用请重启后重配。",
	)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out, "")
	_, _ = fmt.Fprintln(out, "配置摘要：")
	_, _ = fmt.Fprintf(out, "- 主模型: %s\n", summarizeModel(mainModel))
	if assistantConfigured {
		_, _ = fmt.Fprintf(out, "- 助手模型: %s\n", summarizeModel(assistantModel))
	} else {
		_, _ = fmt.Fprintln(out, "- 助手模型: 未配置独立模型（复用主模型）")
	}
	if visionConfigured {
		_, _ = fmt.Fprintf(out, "- 视觉模型: %s\n", summarizeModel(visionModel))
	} else {
		_, _ = fmt.Fprintln(out, "- 视觉模型: 未配置（图片/表情识别不可用）")
	}
	if onlineConfigured {
		_, _ = fmt.Fprintf(out, "- 联网模型: %s\n", summarizeModel(onlineModel))
	} else {
		_, _ = fmt.Fprintln(out, "- 联网模型: 未配置（联网检索不可用）")
	}
	confirm, err := promptLine(reader, out, "确认写入本地覆盖文件并应用本次配置？[Y/n]: ")
	if err != nil {
		return err
	}
	confirm = strings.ToLower(strings.TrimSpace(confirm))
	if confirm == "n" || confirm == "no" {
		return errors.New("model setup canceled by user")
	}

	applyModelToMap(localMap, "llm", mainModel)
	if assistantConfigured {
		applyModelToMap(localMap, "assistant_llm", assistantModel)
	} else {
		delete(localMap, "assistant_llm")
	}
	if visionConfigured {
		applyModelToMap(localMap, "vision_llm", visionModel)
		setNestedBool(localMap, "vision", "enabled", true)
	} else {
		delete(localMap, "vision_llm")
		setNestedBool(localMap, "vision", "enabled", false)
	}
	if onlineConfigured {
		applyModelToMap(localMap, "online_llm", onlineModel)
		setNestedBool(localMap, "online", "enabled", true)
	} else {
		delete(localMap, "online_llm")
		setNestedBool(localMap, "online", "enabled", false)
	}

	if err := writeJSONMap(localPath, localMap); err != nil {
		return err
	}

	if logger != nil {
		logger.Printf("model setup saved to %s", localPath)
		if !assistantConfigured {
			logger.Printf("hint: assistant model not configured (using main model). run with -setup-models to add it later.")
		}
		if !visionConfigured {
			logger.Printf("hint: vision model not configured, image/emoji recognition is disabled. run with -setup-models to enable later.")
		}
		if !onlineConfigured {
			logger.Printf("hint: online model not configured, online search is disabled. run with -setup-models to enable later.")
		}
	}
	return nil
}

func promptRequiredModelStep(reader *bufio.Reader, out io.Writer, step int, title string, current modelInput) (modelInput, error) {
	_, _ = fmt.Fprintln(out, "")
	_, _ = fmt.Fprintf(out, "[Step %d/4] %s\n", step, title)
	if isModelConfigured(current) {
		_, _ = fmt.Fprintf(out, "当前配置: %s\n", summarizeModel(current))
	}
	_, _ = fmt.Fprintln(out, "请配置 base_url、api_key、model。provider 会自动推断，也可手动输入。")
	return promptModelInput(reader, out, current, true)
}

func promptOptionalModelStep(
	reader *bufio.Reader,
	out io.Writer,
	step int,
	title string,
	current modelInput,
	skipHint string,
) (modelInput, bool, error) {
	_, _ = fmt.Fprintln(out, "")
	_, _ = fmt.Fprintf(out, "[Step %d/4] %s\n", step, title)
	hasCurrent := isModelConfigured(current)
	if hasCurrent {
		_, _ = fmt.Fprintf(out, "当前配置: %s\n", summarizeModel(current))
		_, _ = fmt.Fprintf(out, "输入 c=重新配置, k=保留当前, s=跳过。默认 k\n")
	} else {
		_, _ = fmt.Fprintln(out, "当前未配置。")
		_, _ = fmt.Fprintf(out, "输入 c=现在配置, s=跳过。默认 s\n")
	}
	_, _ = fmt.Fprintf(out, "提示: %s\n", skipHint)

	for {
		raw, err := promptLine(reader, out, "选择: ")
		if err != nil {
			return modelInput{}, false, err
		}
		choice := strings.ToLower(strings.TrimSpace(raw))
		if hasCurrent {
			if choice == "" || choice == "k" || choice == "keep" {
				return normalizeModelInput(current), true, nil
			}
			if choice == "c" || choice == "config" {
				m, err := promptModelInput(reader, out, current, true)
				return m, true, err
			}
			if choice == "s" || choice == "skip" {
				return modelInput{}, false, nil
			}
		} else {
			if choice == "" || choice == "s" || choice == "skip" {
				return modelInput{}, false, nil
			}
			if choice == "c" || choice == "config" {
				m, err := promptModelInput(reader, out, current, true)
				return m, true, err
			}
		}
		_, _ = fmt.Fprintln(out, "无效输入，请按提示输入。")
	}
}

func promptModelInput(reader *bufio.Reader, out io.Writer, current modelInput, required bool) (modelInput, error) {
	current = normalizeModelInput(current)
	baseURL, err := promptValue(reader, out, "base_url", current.BaseURL, required, false)
	if err != nil {
		return modelInput{}, err
	}
	apiKey, err := promptValue(reader, out, "api_key", current.APIKey, required, true)
	if err != nil {
		return modelInput{}, err
	}
	model, err := promptValue(reader, out, "model", current.Model, required, false)
	if err != nil {
		return modelInput{}, err
	}

	providerDefault := strings.TrimSpace(current.Provider)
	if providerDefault == "" {
		providerDefault = inferProvider(baseURL, model)
	}
	provider, err := promptValue(reader, out, "provider(可选)", providerDefault, false, false)
	if err != nil {
		return modelInput{}, err
	}
	if strings.TrimSpace(provider) == "" {
		provider = inferProvider(baseURL, model)
	}

	outModel := modelInput{
		Provider: strings.TrimSpace(provider),
		BaseURL:  strings.TrimSpace(baseURL),
		APIKey:   strings.TrimSpace(apiKey),
		Model:    strings.TrimSpace(model),
	}
	outModel = normalizeModelInput(outModel)
	if required && !isModelConfigured(outModel) {
		return modelInput{}, errors.New("model config is incomplete")
	}
	return outModel, nil
}

func promptValue(
	reader *bufio.Reader,
	out io.Writer,
	label, defaultValue string,
	required bool,
	secret bool,
) (string, error) {
	defaultValue = strings.TrimSpace(defaultValue)
	for {
		var prompt string
		if defaultValue != "" {
			if secret {
				prompt = fmt.Sprintf("%s [留空保持当前(%s)]: ", label, maskSecret(defaultValue))
			} else {
				prompt = fmt.Sprintf("%s [默认: %s]: ", label, defaultValue)
			}
		} else {
			prompt = fmt.Sprintf("%s: ", label)
		}

		raw, err := promptLine(reader, out, prompt)
		if err != nil {
			return "", err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			if defaultValue != "" {
				return defaultValue, nil
			}
			if required {
				_, _ = fmt.Fprintf(out, "%s 为必填项。\n", label)
				continue
			}
			return "", nil
		}
		return raw, nil
	}
}

func promptLine(reader *bufio.Reader, out io.Writer, prompt string) (string, error) {
	_, _ = fmt.Fprint(out, prompt)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if errors.Is(err, io.EOF) && line == "" {
		return "", io.EOF
	}
	return line, nil
}

func mergeModelOverride(base, local config.LLMOverride) modelInput {
	out := modelInput{
		Provider: strings.TrimSpace(base.Provider),
		BaseURL:  strings.TrimSpace(base.BaseURL),
		APIKey:   strings.TrimSpace(base.APIKey),
		Model:    strings.TrimSpace(base.Model),
	}
	if v := strings.TrimSpace(local.Provider); v != "" {
		out.Provider = v
	}
	if v := strings.TrimSpace(local.BaseURL); v != "" {
		out.BaseURL = v
	}
	if v := strings.TrimSpace(local.APIKey); v != "" {
		out.APIKey = v
	}
	if v := strings.TrimSpace(local.Model); v != "" {
		out.Model = v
	}
	return normalizeModelInput(out)
}

func normalizeModelInput(m modelInput) modelInput {
	m.Provider = strings.TrimSpace(m.Provider)
	m.BaseURL = strings.TrimSpace(m.BaseURL)
	m.APIKey = strings.TrimSpace(m.APIKey)
	m.Model = strings.TrimSpace(m.Model)
	if m.Provider == "" {
		m.Provider = inferProvider(m.BaseURL, m.Model)
	}
	return m
}

func isModelConfigured(m modelInput) bool {
	return strings.TrimSpace(m.BaseURL) != "" &&
		strings.TrimSpace(m.APIKey) != "" &&
		strings.TrimSpace(m.Model) != ""
}

func summarizeModel(m modelInput) string {
	m = normalizeModelInput(m)
	return fmt.Sprintf("provider=%s model=%s base_url=%s api_key=%s",
		m.Provider, m.Model, m.BaseURL, maskSecret(m.APIKey))
}

func inferProvider(baseURL, model string) string {
	raw := strings.ToLower(strings.TrimSpace(baseURL + " " + model))
	switch {
	case strings.Contains(raw, "moonshot"), strings.Contains(raw, "kimi"):
		return "moonshot"
	case strings.Contains(raw, "deepseek"):
		return "deepseek"
	case strings.Contains(raw, "bigmodel"), strings.Contains(raw, "zhipu"), strings.Contains(raw, "glm"):
		return "zhipu"
	case strings.Contains(raw, "dashscope"), strings.Contains(raw, "qwen"):
		return "qwen"
	case strings.Contains(raw, "openai"):
		return "openai"
	default:
		return "custom"
	}
}

func maskSecret(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "(empty)"
	}
	if strings.HasPrefix(strings.ToLower(v), "env:") {
		return v
	}
	r := []rune(v)
	if len(r) <= 8 {
		return strings.Repeat("*", len(r))
	}
	return string(r[:4]) + "..." + string(r[len(r)-4:])
}

func applyModelToMap(root map[string]any, key string, model modelInput) {
	section := ensureMapSection(root, key)
	model = normalizeModelInput(model)
	section["provider"] = model.Provider
	section["base_url"] = model.BaseURL
	section["api_key"] = model.APIKey
	section["model"] = model.Model
}

func setNestedBool(root map[string]any, section, key string, value bool) {
	target := ensureMapSection(root, section)
	target[key] = value
}

func ensureMapSection(root map[string]any, key string) map[string]any {
	if root == nil {
		return map[string]any{}
	}
	if raw, ok := root[key]; ok {
		if m, ok := raw.(map[string]any); ok {
			return m
		}
	}
	m := make(map[string]any)
	root[key] = m
	return m
}

func readJSONMap(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("read json file failed: %w", err)
	}
	b = stripBOM(b)
	if len(strings.TrimSpace(string(b))) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode json file failed (%s): %w", path, err)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func writeJSONMap(path string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir failed: %w", err)
	}
	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config failed: %w", err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("write config file failed: %w", err)
	}
	return nil
}

func stripBOM(b []byte) []byte {
	utf8BOM := []byte{0xEF, 0xBB, 0xBF}
	if len(b) >= len(utf8BOM) && bytes.Equal(b[:len(utf8BOM)], utf8BOM) {
		return b[len(utf8BOM):]
	}
	return b
}
