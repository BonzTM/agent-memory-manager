package runtime

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultSummarizerBatchSize      = 20
	defaultReflectBatchSize         = 100
	defaultReflectLLMBatchSize      = 20
	defaultLifecycleReviewBatchSize = 50
)

// Config holds all runtime configuration for amm.
// Matches blueprint section 15.
type Config struct {
	Storage     StorageConfig     `json:"storage"`
	Retrieval   RetrievalConfig   `json:"retrieval"`
	Privacy     PrivacyConfig     `json:"privacy"`
	Maintenance MaintenanceConfig `json:"maintenance"`
	Summarizer  SummarizerConfig  `json:"summarizer"`
	Embeddings  EmbeddingsConfig  `json:"embeddings"`
}

type SummarizerConfig struct {
	Endpoint                 string `json:"endpoint"`
	APIKey                   string `json:"api_key"`
	Model                    string `json:"model"`
	ReviewEndpoint           string `json:"review_endpoint"`
	ReviewAPIKey             string `json:"review_api_key"`
	ReviewModel              string `json:"review_model"`
	BatchSize                int    `json:"batch_size"`
	ReflectBatchSize         int    `json:"reflect_batch_size"`
	ReflectLLMBatchSize      int    `json:"reflect_llm_batch_size"`
	LifecycleReviewBatchSize int    `json:"lifecycle_review_batch_size"`
}

type EmbeddingsConfig struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

// StorageConfig controls where amm persists data.
type StorageConfig struct {
	DBPath string `json:"db_path"`
}

// RetrievalConfig tunes recall behavior.
type RetrievalConfig struct {
	DefaultLimit   int  `json:"default_limit"`
	AmbientLimit   int  `json:"ambient_limit"`
	EnableSemantic bool `json:"enable_semantic"`
	EnableExplain  bool `json:"enable_explain"`
}

// PrivacyConfig sets default privacy behavior.
type PrivacyConfig struct {
	DefaultPrivacy string `json:"default_privacy"`
}

// MaintenanceConfig controls automatic maintenance jobs.
type MaintenanceConfig struct {
	AutoReflect              bool `json:"auto_reflect"`
	AutoCompress             bool `json:"auto_compress"`
	AutoConsolidate          bool `json:"auto_consolidate"`
	AutoDetectContradictions bool `json:"auto_detect_contradictions"`
}

// DefaultConfig returns sensible defaults matching the blueprint.
func DefaultConfig() Config {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return Config{
		Storage: StorageConfig{
			DBPath: filepath.Join(home, ".amm", "amm.db"),
		},
		Retrieval: RetrievalConfig{
			DefaultLimit:   10,
			AmbientLimit:   5,
			EnableSemantic: false,
			EnableExplain:  true,
		},
		Privacy: PrivacyConfig{
			DefaultPrivacy: "private",
		},
		Maintenance: MaintenanceConfig{
			AutoReflect:              true,
			AutoCompress:             true,
			AutoConsolidate:          true,
			AutoDetectContradictions: true,
		},
		Summarizer: SummarizerConfig{
			BatchSize:                defaultSummarizerBatchSize,
			ReflectBatchSize:         defaultReflectBatchSize,
			ReflectLLMBatchSize:      defaultReflectLLMBatchSize,
			LifecycleReviewBatchSize: defaultLifecycleReviewBatchSize,
		},
		Embeddings: EmbeddingsConfig{
			Enabled: false,
		},
	}
}

// LoadConfig reads a config file (JSON or TOML) and merges it with defaults.
// If the file does not exist, defaults are returned without error.
// JSON is detected by leading '{'; otherwise basic TOML (flat key = "value"
// with [section] headers) is assumed.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		// JSON format.
		if err := json.Unmarshal(data, &cfg); err != nil {
			return DefaultConfig(), err
		}
		return cfg, nil
	}

	// Basic TOML parser: supports [section] headers and key = "value" pairs.
	if err := parseFlatTOML(data, &cfg); err != nil {
		return DefaultConfig(), err
	}
	return cfg, nil
}

// parseFlatTOML handles a simple subset of TOML sufficient for amm config:
// [section] headers and key = "value" / key = number / key = bool lines.
func parseFlatTOML(data []byte, cfg *Config) error {
	section := ""
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Strip surrounding quotes.
		val = strings.Trim(val, "\"'")

		fqKey := section + "." + key
		switch fqKey {
		case "storage.db_path":
			cfg.Storage.DBPath = val
		case "retrieval.default_limit":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.Retrieval.DefaultLimit = n
			}
		case "retrieval.ambient_limit":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.Retrieval.AmbientLimit = n
			}
		case "retrieval.enable_semantic":
			if b, err := strconv.ParseBool(val); err == nil {
				cfg.Retrieval.EnableSemantic = b
			}
		case "retrieval.enable_explain":
			if b, err := strconv.ParseBool(val); err == nil {
				cfg.Retrieval.EnableExplain = b
			}
		case "privacy.default_privacy":
			cfg.Privacy.DefaultPrivacy = val
		case "maintenance.auto_reflect":
			if b, err := strconv.ParseBool(val); err == nil {
				cfg.Maintenance.AutoReflect = b
			}
		case "maintenance.auto_compress":
			if b, err := strconv.ParseBool(val); err == nil {
				cfg.Maintenance.AutoCompress = b
			}
		case "maintenance.auto_consolidate":
			if b, err := strconv.ParseBool(val); err == nil {
				cfg.Maintenance.AutoConsolidate = b
			}
		case "maintenance.auto_detect_contradictions":
			if b, err := strconv.ParseBool(val); err == nil {
				cfg.Maintenance.AutoDetectContradictions = b
			}
		case "summarizer.endpoint":
			cfg.Summarizer.Endpoint = val
		case "summarizer.api_key":
			cfg.Summarizer.APIKey = val
		case "summarizer.model":
			cfg.Summarizer.Model = val
		case "summarizer.batch_size":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Summarizer.BatchSize = n
			}
		case "summarizer.review_endpoint":
			cfg.Summarizer.ReviewEndpoint = val
		case "summarizer.review_api_key":
			cfg.Summarizer.ReviewAPIKey = val
		case "summarizer.review_model":
			cfg.Summarizer.ReviewModel = val
		case "summarizer.reflect_batch_size":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Summarizer.ReflectBatchSize = n
			}
		case "summarizer.reflect_llm_batch_size":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Summarizer.ReflectLLMBatchSize = n
			}
		case "summarizer.lifecycle_review_batch_size":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Summarizer.LifecycleReviewBatchSize = n
			}
		case "embeddings.enabled":
			if b, err := strconv.ParseBool(val); err == nil {
				cfg.Embeddings.Enabled = b
			}
		case "embeddings.provider":
			cfg.Embeddings.Provider = val
		case "embeddings.endpoint":
			cfg.Embeddings.Endpoint = val
		case "embeddings.api_key":
			cfg.Embeddings.APIKey = val
		case "embeddings.model":
			cfg.Embeddings.Model = val
		}
	}
	return scanner.Err()
}

// LoadConfigWithEnv loads configuration by merging defaults, config file, and
// environment variables (in that priority order). It checks ~/.amm/config.json,
// ~/.amm/config.toml, and AMM_DB_PATH for the config file location.
func LoadConfigWithEnv() Config {
	cfg := DefaultConfig()

	home, _ := os.UserHomeDir()
	if home != "" {
		for _, name := range []string{"config.json", "config.toml"} {
			path := filepath.Join(home, ".amm", name)
			if fileCfg, err := LoadConfig(path); err == nil {
				cfg = fileCfg
				break
			}
		}
	}

	cfg = ConfigFromEnv(cfg)
	return cfg
}

// ConfigFromEnv overrides config fields with environment variables.
// Supported variables:
//
//	AMM_DB_PATH           -> Storage.DBPath
//	AMM_DEFAULT_LIMIT     -> Retrieval.DefaultLimit
//	AMM_AMBIENT_LIMIT     -> Retrieval.AmbientLimit
//	AMM_ENABLE_SEMANTIC   -> Retrieval.EnableSemantic (true/false)
//	AMM_ENABLE_EXPLAIN    -> Retrieval.EnableExplain  (true/false)
//	AMM_DEFAULT_PRIVACY   -> Privacy.DefaultPrivacy
//	AMM_AUTO_REFLECT      -> Maintenance.AutoReflect  (true/false)
//	AMM_AUTO_COMPRESS     -> Maintenance.AutoCompress  (true/false)
//	AMM_AUTO_CONSOLIDATE  -> Maintenance.AutoConsolidate (true/false)
//	AMM_AUTO_DETECT_CONTRADICTIONS -> Maintenance.AutoDetectContradictions (true/false)
//	AMM_SUMMARIZER_ENDPOINT -> Summarizer.Endpoint
//	AMM_SUMMARIZER_API_KEY -> Summarizer.APIKey
//	AMM_SUMMARIZER_MODEL -> Summarizer.Model
//	AMM_SUMMARIZER_BATCH_SIZE -> Summarizer.BatchSize
//	AMM_EMBEDDINGS_ENABLED -> Embeddings.Enabled (true/false)
//	AMM_EMBEDDINGS_PROVIDER -> Embeddings.Provider
//	AMM_EMBEDDINGS_ENDPOINT -> Embeddings.Endpoint
//	AMM_EMBEDDINGS_API_KEY -> Embeddings.APIKey
//	AMM_EMBEDDINGS_MODEL -> Embeddings.Model
func ConfigFromEnv(base Config) Config {
	if v := os.Getenv("AMM_DB_PATH"); v != "" {
		base.Storage.DBPath = v
	}
	if v := os.Getenv("AMM_DEFAULT_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Retrieval.DefaultLimit = n
		}
	}
	if v := os.Getenv("AMM_AMBIENT_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Retrieval.AmbientLimit = n
		}
	}
	if v := os.Getenv("AMM_ENABLE_SEMANTIC"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			base.Retrieval.EnableSemantic = b
		}
	}
	if v := os.Getenv("AMM_ENABLE_EXPLAIN"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			base.Retrieval.EnableExplain = b
		}
	}
	if v := os.Getenv("AMM_DEFAULT_PRIVACY"); v != "" {
		base.Privacy.DefaultPrivacy = v
	}
	if v := os.Getenv("AMM_AUTO_REFLECT"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			base.Maintenance.AutoReflect = b
		}
	}
	if v := os.Getenv("AMM_AUTO_COMPRESS"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			base.Maintenance.AutoCompress = b
		}
	}
	if v := os.Getenv("AMM_AUTO_CONSOLIDATE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			base.Maintenance.AutoConsolidate = b
		}
	}
	if v := os.Getenv("AMM_AUTO_DETECT_CONTRADICTIONS"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			base.Maintenance.AutoDetectContradictions = b
		}
	}
	if v := os.Getenv("AMM_SUMMARIZER_ENDPOINT"); v != "" {
		base.Summarizer.Endpoint = v
	}
	if v := os.Getenv("AMM_SUMMARIZER_API_KEY"); v != "" {
		base.Summarizer.APIKey = v
	}
	if v := os.Getenv("AMM_SUMMARIZER_MODEL"); v != "" {
		base.Summarizer.Model = v
	}
	if v := os.Getenv("AMM_SUMMARIZER_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Summarizer.BatchSize = n
		}
	}
	if v := os.Getenv("AMM_REVIEW_ENDPOINT"); v != "" {
		base.Summarizer.ReviewEndpoint = v
	}
	if v := os.Getenv("AMM_REVIEW_API_KEY"); v != "" {
		base.Summarizer.ReviewAPIKey = v
	}
	if v := os.Getenv("AMM_REVIEW_MODEL"); v != "" {
		base.Summarizer.ReviewModel = v
	}
	if v := os.Getenv("AMM_REFLECT_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Summarizer.ReflectBatchSize = n
		}
	}
	if v := os.Getenv("AMM_REFLECT_LLM_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Summarizer.ReflectLLMBatchSize = n
		}
	}
	if v := os.Getenv("AMM_LIFECYCLE_REVIEW_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Summarizer.LifecycleReviewBatchSize = n
		}
	}
	if v := os.Getenv("AMM_EMBEDDINGS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			base.Embeddings.Enabled = b
		}
	}
	if v := os.Getenv("AMM_EMBEDDINGS_PROVIDER"); v != "" {
		base.Embeddings.Provider = v
	}
	if v := os.Getenv("AMM_EMBEDDINGS_ENDPOINT"); v != "" {
		base.Embeddings.Endpoint = v
	}
	if v := os.Getenv("AMM_EMBEDDINGS_API_KEY"); v != "" {
		base.Embeddings.APIKey = v
	}
	if v := os.Getenv("AMM_EMBEDDINGS_MODEL"); v != "" {
		base.Embeddings.Model = v
	}
	return base
}
