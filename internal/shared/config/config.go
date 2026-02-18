package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ListenEntry struct {
	Nickname string `json:"nickname"`
	Prompt   string `json:"prompt"`
}

type ModelConfig struct {
	Provider       string
	BaseURL        string
	APIKey         string
	Model          string
	Temperature    float64
	MaxTokens      int
	TimeoutSeconds int
}

type GroupPolicyConfig struct {
	AcceptAll           bool
	EnableAtReply       bool
	EnableKeywordReply  bool
	KeywordList         []string
	ResponseProbability int
}

type EmojiConfig struct {
	Enabled         bool
	Dir             string
	SendProbability int
}

type VisionFeatureConfig struct {
	Enabled     bool
	ImagePrompt string
	EmojiPrompt string
}

type OnlineConfig struct {
	Enabled         bool
	DetectionPrompt string
	FixedPrompt     string
}

type AppConfig struct {
	Port               int
	QueueWaitingTime   int
	QueueMaxMessages   int
	QueueWorkers       int
	MaxGroups          int
	EnableTextCommands bool
	ListenList         []ListenEntry
	MainModel          ModelConfig
	AssistantModel     ModelConfig
	VisionModel        ModelConfig
	OnlineModel        ModelConfig
	GroupPolicy        GroupPolicyConfig
	Emoji              EmojiConfig
	Vision             VisionFeatureConfig
	Online             OnlineConfig
}

type LocalRuntimeConfig struct {
	Port               int                      `json:"port"`
	QueueWaitingTime   int                      `json:"queue_waiting_time"`
	QueueMaxMessages   int                      `json:"queue_max_messages_per_user"`
	QueueWorkers       int                      `json:"queue_worker_concurrency"`
	MaxGroups          int                      `json:"max_groups"`
	EnableTextCommands *bool                    `json:"enable_text_commands"`
	ListenList         []ListenEntry            `json:"listen_list"`
	GroupPolicy        GroupPolicyConfigOverlay `json:"group_policy"`
	LLM                LLMOverride              `json:"llm"`
	AssistantLLM       LLMOverride              `json:"assistant_llm"`
	VisionLLM          LLMOverride              `json:"vision_llm"`
	OnlineLLM          LLMOverride              `json:"online_llm"`
	Emoji              EmojiOverlay             `json:"emoji"`
	Vision             VisionOverlay            `json:"vision"`
	Online             OnlineOverlay            `json:"online"`
}

type GroupPolicyConfigOverlay struct {
	AcceptAll           *bool    `json:"accept_all"`
	EnableAtReply       *bool    `json:"enable_at_reply"`
	EnableKeywordReply  *bool    `json:"enable_keyword_reply"`
	KeywordList         []string `json:"keyword_list"`
	ResponseProbability int      `json:"response_probability"`
}

type LLMOverride struct {
	Provider       string  `json:"provider"`
	BaseURL        string  `json:"base_url"`
	APIKey         string  `json:"api_key"`
	Model          string  `json:"model"`
	Temperature    float64 `json:"temperature"`
	MaxTokens      int     `json:"max_tokens"`
	TimeoutSeconds int     `json:"timeout_seconds"`
}

type EmojiOverlay struct {
	Enabled         *bool  `json:"enabled"`
	Dir             string `json:"dir"`
	SendProbability int    `json:"send_probability"`
}

type VisionOverlay struct {
	Enabled     *bool  `json:"enabled"`
	ImagePrompt string `json:"image_prompt"`
	EmojiPrompt string `json:"emoji_prompt"`
}

type OnlineOverlay struct {
	Enabled         *bool  `json:"enabled"`
	DetectionPrompt string `json:"detection_prompt"`
	FixedPrompt     string `json:"fixed_prompt"`
}

func DefaultAppConfig() AppConfig {
	return AppConfig{
		Port:               5000,
		QueueWaitingTime:   7,
		QueueMaxMessages:   40,
		QueueWorkers:       4,
		MaxGroups:          5,
		EnableTextCommands: true,
		ListenList:         []ListenEntry{},
		MainModel: ModelConfig{
			Temperature:    1.0,
			MaxTokens:      2000,
			TimeoutSeconds: 45,
		},
		GroupPolicy: GroupPolicyConfig{
			AcceptAll:           false,
			EnableAtReply:       true,
			EnableKeywordReply:  true,
			KeywordList:         []string{"你好", "机器人", "在吗"},
			ResponseProbability: 100,
		},
		Emoji: EmojiConfig{
			Enabled:         false,
			Dir:             "emojis",
			SendProbability: 25,
		},
		Vision: VisionFeatureConfig{
			Enabled:     false,
			ImagePrompt: "请用中文描述这张图片的主要内容。直接描述，不要说“这是…”。",
			EmojiPrompt: "请用中文简洁描述该表情包表达的情绪或含义。",
		},
		Online: OnlineConfig{
			Enabled:         false,
			DetectionPrompt: "是否需要查询当前实时外部信息（天气/新闻/股价/近期动态等）",
			FixedPrompt:     "",
		},
	}
}

func LoadStandaloneConfig(path string) (AppConfig, error) {
	local, err := loadLocalRuntimeConfig(path)
	if err != nil {
		return AppConfig{}, err
	}

	cfg := DefaultAppConfig()
	cfg.ApplyLocalRuntimeConfig(local)
	if localOverride, ok, err := loadOptionalLocalOverride(path); err != nil {
		return AppConfig{}, err
	} else if ok {
		cfg.ApplyLocalRuntimeConfig(localOverride)
	}
	cfg.Normalize()
	if err := cfg.ValidateRuntime(); err != nil {
		return AppConfig{}, err
	}
	return cfg, nil
}

func loadOptionalLocalOverride(baseConfigPath string) (LocalRuntimeConfig, bool, error) {
	localPath, err := resolveLocalOverridePath(baseConfigPath)
	if err != nil {
		return LocalRuntimeConfig{}, false, err
	}
	if strings.TrimSpace(localPath) == "" {
		return LocalRuntimeConfig{}, false, nil
	}
	if _, err := os.Stat(localPath); err != nil {
		if os.IsNotExist(err) {
			return LocalRuntimeConfig{}, false, nil
		}
		return LocalRuntimeConfig{}, false, fmt.Errorf("check local override failed: %w", err)
	}
	cfg, err := loadLocalRuntimeConfig(localPath)
	if err != nil {
		return LocalRuntimeConfig{}, false, fmt.Errorf("load local override failed (%s): %w", localPath, err)
	}
	return cfg, true, nil
}

func resolveLocalOverridePath(baseConfigPath string) (string, error) {
	baseConfigPath = strings.TrimSpace(baseConfigPath)
	if baseConfigPath == "" {
		return "", nil
	}
	baseAbs, err := filepath.Abs(baseConfigPath)
	if err != nil {
		return "", err
	}
	baseDir := filepath.Dir(baseAbs)

	if envPath := strings.TrimSpace(os.Getenv("WXBOT_LOCAL_CONFIG")); envPath != "" {
		if !filepath.IsAbs(envPath) {
			envPath = filepath.Join(baseDir, envPath)
		}
		return filepath.Abs(envPath)
	}
	// Backward compatibility with previous project name.
	if envPath := strings.TrimSpace(os.Getenv("WECHATBOT_LOCAL_CONFIG")); envPath != "" {
		if !filepath.IsAbs(envPath) {
			envPath = filepath.Join(baseDir, envPath)
		}
		return filepath.Abs(envPath)
	}
	return filepath.Join(baseDir, "config.local.json"), nil
}

// ResolveLocalOverridePath returns the local override config path for a base config file.
// It applies the same rules as runtime loading:
// 1) WXBOT_LOCAL_CONFIG if set (relative to base config dir)
// 2) WECHATBOT_LOCAL_CONFIG if set (backward compatibility)
// 3) <base config dir>/config.local.json
func ResolveLocalOverridePath(baseConfigPath string) (string, error) {
	return resolveLocalOverridePath(baseConfigPath)
}

func loadLocalRuntimeConfig(path string) (LocalRuntimeConfig, error) {
	if strings.TrimSpace(path) == "" {
		return LocalRuntimeConfig{}, errors.New("bot config path is empty")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return LocalRuntimeConfig{}, fmt.Errorf("read bot config failed: %w", err)
	}
	b = stripUTF8BOM(b)
	if len(strings.TrimSpace(string(b))) == 0 {
		return LocalRuntimeConfig{}, errors.New("bot config file is empty")
	}
	var cfg LocalRuntimeConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return LocalRuntimeConfig{}, fmt.Errorf("decode bot config failed: %w", err)
	}
	return cfg, nil
}

// LoadLocalRuntimeConfig reads a standalone runtime config JSON file.
// This is useful for setup tools that need to read config.json/config.local.json
// before full runtime validation.
func LoadLocalRuntimeConfig(path string) (LocalRuntimeConfig, error) {
	return loadLocalRuntimeConfig(path)
}

func stripUTF8BOM(b []byte) []byte {
	utf8BOM := []byte{0xEF, 0xBB, 0xBF}
	if len(b) >= 3 && bytes.Equal(b[:3], utf8BOM) {
		return b[3:]
	}
	return b
}

func (c *AppConfig) ApplyLocalRuntimeConfig(local LocalRuntimeConfig) {
	if local.Port > 0 {
		c.Port = local.Port
	}
	if local.QueueWaitingTime > 0 {
		c.QueueWaitingTime = local.QueueWaitingTime
	}
	if local.QueueMaxMessages > 0 {
		c.QueueMaxMessages = local.QueueMaxMessages
	}
	if local.QueueWorkers > 0 {
		c.QueueWorkers = local.QueueWorkers
	}
	if local.MaxGroups > 0 {
		c.MaxGroups = local.MaxGroups
	}
	if local.EnableTextCommands != nil {
		c.EnableTextCommands = *local.EnableTextCommands
	}

	if len(local.ListenList) > 0 {
		items := make([]ListenEntry, 0, len(local.ListenList))
		for _, row := range local.ListenList {
			nick := strings.TrimSpace(row.Nickname)
			prompt := strings.TrimSpace(row.Prompt)
			if nick == "" || prompt == "" {
				continue
			}
			items = append(items, ListenEntry{Nickname: nick, Prompt: prompt})
		}
		if len(items) > 0 {
			c.ListenList = items
		}
	}

	if local.GroupPolicy.AcceptAll != nil {
		c.GroupPolicy.AcceptAll = *local.GroupPolicy.AcceptAll
	}
	if local.GroupPolicy.EnableAtReply != nil {
		c.GroupPolicy.EnableAtReply = *local.GroupPolicy.EnableAtReply
	}
	if local.GroupPolicy.EnableKeywordReply != nil {
		c.GroupPolicy.EnableKeywordReply = *local.GroupPolicy.EnableKeywordReply
	}
	if len(local.GroupPolicy.KeywordList) > 0 {
		c.GroupPolicy.KeywordList = local.GroupPolicy.KeywordList
	}
	if local.GroupPolicy.ResponseProbability > 0 {
		c.GroupPolicy.ResponseProbability = local.GroupPolicy.ResponseProbability
	}

	applyModelOverride(&c.MainModel, local.LLM)
	applyModelOverride(&c.AssistantModel, local.AssistantLLM)
	applyModelOverride(&c.VisionModel, local.VisionLLM)
	applyModelOverride(&c.OnlineModel, local.OnlineLLM)

	if local.Emoji.Enabled != nil {
		c.Emoji.Enabled = *local.Emoji.Enabled
	}
	if strings.TrimSpace(local.Emoji.Dir) != "" {
		c.Emoji.Dir = strings.TrimSpace(local.Emoji.Dir)
	}
	if local.Emoji.SendProbability > 0 {
		c.Emoji.SendProbability = local.Emoji.SendProbability
	}

	if local.Vision.Enabled != nil {
		c.Vision.Enabled = *local.Vision.Enabled
	}
	if strings.TrimSpace(local.Vision.ImagePrompt) != "" {
		c.Vision.ImagePrompt = strings.TrimSpace(local.Vision.ImagePrompt)
	}
	if strings.TrimSpace(local.Vision.EmojiPrompt) != "" {
		c.Vision.EmojiPrompt = strings.TrimSpace(local.Vision.EmojiPrompt)
	}

	if local.Online.Enabled != nil {
		c.Online.Enabled = *local.Online.Enabled
	}
	if strings.TrimSpace(local.Online.DetectionPrompt) != "" {
		c.Online.DetectionPrompt = strings.TrimSpace(local.Online.DetectionPrompt)
	}
	if local.Online.FixedPrompt != "" {
		c.Online.FixedPrompt = local.Online.FixedPrompt
	}
}

func (c *AppConfig) Normalize() {
	if c.Port <= 0 {
		c.Port = 5000
	}
	if c.QueueWaitingTime <= 0 {
		c.QueueWaitingTime = 7
	}
	if c.QueueMaxMessages <= 0 {
		c.QueueMaxMessages = 40
	}
	if c.QueueWorkers <= 0 {
		c.QueueWorkers = 4
	}
	if c.MaxGroups <= 0 {
		c.MaxGroups = 5
	}

	resolveModelEnvRefs(&c.MainModel)
	resolveModelEnvRefs(&c.AssistantModel)
	resolveModelEnvRefs(&c.VisionModel)
	resolveModelEnvRefs(&c.OnlineModel)

	normalizeModelDefaults(&c.MainModel)
	c.AssistantModel.FillMissingFrom(c.MainModel)
	normalizeModelDefaults(&c.AssistantModel)
	c.VisionModel.FillMissingFrom(c.MainModel)
	normalizeModelDefaults(&c.VisionModel)
	c.OnlineModel.FillMissingFrom(c.MainModel)
	normalizeModelDefaults(&c.OnlineModel)

	if c.GroupPolicy.ResponseProbability <= 0 || c.GroupPolicy.ResponseProbability > 100 {
		c.GroupPolicy.ResponseProbability = 100
	}
	if len(c.GroupPolicy.KeywordList) == 0 {
		c.GroupPolicy.KeywordList = []string{"你好", "机器人", "在吗"}
	}

	if c.Emoji.Dir == "" {
		c.Emoji.Dir = "emojis"
	}
	if c.Emoji.SendProbability <= 0 || c.Emoji.SendProbability > 100 {
		c.Emoji.SendProbability = 25
	}
	if strings.TrimSpace(c.Vision.ImagePrompt) == "" {
		c.Vision.ImagePrompt = "请用中文描述这张图片的主要内容。直接描述，不要说“这是…”。"
	}
	if strings.TrimSpace(c.Vision.EmojiPrompt) == "" {
		c.Vision.EmojiPrompt = "请用中文简洁描述该表情包表达的情绪或含义。"
	}
	if strings.TrimSpace(c.Online.DetectionPrompt) == "" {
		c.Online.DetectionPrompt = "是否需要查询当前实时外部信息（天气/新闻/股价/近期动态等）"
	}
}

func normalizeModelDefaults(m *ModelConfig) {
	if m.MaxTokens <= 0 {
		m.MaxTokens = 2000
	}
	if m.TimeoutSeconds <= 0 {
		m.TimeoutSeconds = 45
	}
	if m.Temperature == 0 {
		m.Temperature = 1.0
	}
}

func resolveModelEnvRefs(m *ModelConfig) {
	m.Provider = resolveEnvRef(m.Provider)
	m.BaseURL = resolveEnvRef(m.BaseURL)
	m.APIKey = resolveEnvRef(m.APIKey)
	m.Model = resolveEnvRef(m.Model)
}

func resolveEnvRef(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	lower := strings.ToLower(value)
	if !strings.HasPrefix(lower, "env:") {
		return value
	}
	key := strings.TrimSpace(value[4:])
	if key == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(key))
}

func applyModelOverride(dst *ModelConfig, src LLMOverride) {
	if strings.TrimSpace(src.Provider) != "" {
		dst.Provider = strings.TrimSpace(src.Provider)
	}
	if strings.TrimSpace(src.BaseURL) != "" {
		dst.BaseURL = strings.TrimSpace(src.BaseURL)
	}
	if strings.TrimSpace(src.APIKey) != "" {
		dst.APIKey = strings.TrimSpace(src.APIKey)
	}
	if strings.TrimSpace(src.Model) != "" {
		dst.Model = strings.TrimSpace(src.Model)
	}
	if src.Temperature != 0 {
		dst.Temperature = src.Temperature
	}
	if src.MaxTokens > 0 {
		dst.MaxTokens = src.MaxTokens
	}
	if src.TimeoutSeconds > 0 {
		dst.TimeoutSeconds = src.TimeoutSeconds
	}
}

func (m *ModelConfig) FillMissingFrom(base ModelConfig) {
	if strings.TrimSpace(m.Provider) == "" {
		m.Provider = base.Provider
	}
	if strings.TrimSpace(m.BaseURL) == "" {
		m.BaseURL = base.BaseURL
	}
	if strings.TrimSpace(m.APIKey) == "" {
		m.APIKey = base.APIKey
	}
	if strings.TrimSpace(m.Model) == "" {
		m.Model = base.Model
	}
	if m.Temperature == 0 {
		m.Temperature = base.Temperature
	}
	if m.MaxTokens <= 0 {
		m.MaxTokens = base.MaxTokens
	}
	if m.TimeoutSeconds <= 0 {
		m.TimeoutSeconds = base.TimeoutSeconds
	}
}

func (m ModelConfig) IsComplete() bool {
	return strings.TrimSpace(m.BaseURL) != "" &&
		strings.TrimSpace(m.APIKey) != "" &&
		strings.TrimSpace(m.Model) != ""
}

func (c AppConfig) ValidateRuntime() error {
	if len(c.ListenList) == 0 {
		return errors.New("listen_list is empty")
	}
	for _, row := range c.ListenList {
		if strings.TrimSpace(row.Nickname) == "" || strings.TrimSpace(row.Prompt) == "" {
			return errors.New("listen_list contains empty nickname or prompt")
		}
	}
	if !c.MainModel.IsComplete() {
		return errors.New("main model config is incomplete (base_url/api_key/model)")
	}
	if c.Vision.Enabled && !c.VisionModel.IsComplete() {
		return errors.New("vision model config is incomplete (base_url/api_key/model)")
	}
	if c.Online.Enabled && !c.OnlineModel.IsComplete() {
		return errors.New("online model config is incomplete (base_url/api_key/model)")
	}
	if c.Emoji.Enabled && strings.TrimSpace(c.Emoji.Dir) == "" {
		return errors.New("emoji.dir is empty")
	}
	return nil
}
