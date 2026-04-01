package runtime

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultReprocessBatchSize              = 20
	defaultReflectBatchSize                = 100
	defaultReflectLLMBatchSize             = 20
	defaultLifecycleReviewBatchSize        = 50
	defaultCompressChunkSize               = 10
	defaultCompressMaxEvents               = 200
	defaultCompressBatchSize               = 15
	defaultTopicBatchSize                  = 15
	defaultEmbeddingBatchSize              = 64
	defaultCrossProjectSimilarity          = 0.7
	defaultEscalationDeterministicMaxChars = 2048
	defaultMaxExpandDepth                  = 1
	defaultMinConfidenceForCreation        = 0.5
	defaultMinImportanceForCreation        = 0.3
	defaultSessionIdleTimeoutMinutes      = 15
	defaultSummarizerContextWindow        = 128000
	defaultEntityHubThreshold              = 10
)

// Config holds all runtime configuration for amm.
// Matches blueprint section 15.
type Config struct {
	Storage        StorageConfig        `json:"storage"`
	Retrieval      RetrievalConfig      `json:"retrieval"`
	Privacy        PrivacyConfig        `json:"privacy"`
	Maintenance    MaintenanceConfig    `json:"maintenance"`
	Compression    CompressionConfig    `json:"compression"`
	Summarizer     SummarizerConfig     `json:"summarizer"`
	Embeddings     EmbeddingsConfig     `json:"embeddings"`
	IntakeQuality  IntakeQualityConfig  `json:"intake_quality"`
	MaxExpandDepth int                  `json:"max_expand_depth"`
	HTTP           HTTPConfig           `json:"http"`
	API            APIConfig            `json:"api"`
}

// IntakeQualityConfig controls minimum thresholds for memory creation.
type IntakeQualityConfig struct {
	MinConfidenceForCreation float64 `json:"min_confidence_for_creation"`
	MinImportanceForCreation float64 `json:"min_importance_for_creation"`
}

// APIConfig controls remote API client mode. When URL is set, CLI and MCP
// binaries act as HTTP clients to a remote amm-http server instead of
// opening a local database.
type APIConfig struct {
	URL string `json:"url"` // Remote amm-http server base URL (e.g. http://localhost:8080)
	Key string `json:"key"` // API key for authenticating with the remote server
}

type CompressionConfig struct {
	EscalationDeterministicMaxChars int `json:"escalation_deterministic_max_chars"`
}

type HTTPConfig struct {
	Addr        string `json:"addr"`
	CORSOrigins string `json:"cors_origins"`
}

type SummarizerConfig struct {
	Endpoint                        string  `json:"endpoint"`
	APIKey                          string  `json:"api_key"`
	Model                           string  `json:"model"`
	ReviewEndpoint                  string  `json:"review_endpoint"`
	ReviewAPIKey                    string  `json:"review_api_key"`
	ReviewModel                     string  `json:"review_model"`
	ReprocessBatchSize              int     `json:"reprocess_batch_size"`
	ReflectBatchSize                int     `json:"reflect_batch_size"`
	ReflectLLMBatchSize             int     `json:"reflect_llm_batch_size"`
	LifecycleReviewBatchSize        int     `json:"lifecycle_review_batch_size"`
	CompressChunkSize               int     `json:"compress_chunk_size"`
	CompressMaxEvents               int     `json:"compress_max_events"`
	CompressBatchSize               int     `json:"compress_batch_size"`
	TopicBatchSize                  int     `json:"topic_batch_size"`
	EmbeddingBatchSize              int     `json:"embedding_batch_size"`
	CrossProjectSimilarityThreshold float64 `json:"cross_project_similarity_threshold"`
	SessionIdleTimeoutMinutes       int     `json:"session_idle_timeout_minutes"`
	SummarizerContextWindow         int     `json:"summarizer_context_window"`
}

type EmbeddingsConfig struct {
	Enabled            bool   `json:"enabled"`
	ExplicitlyDisabled bool   `json:"-"` // true when operator set AMM_EMBEDDINGS_ENABLED=false
	Provider           string `json:"provider"`
	Endpoint           string `json:"endpoint"`
	APIKey             string `json:"api_key"`
	Model              string `json:"model"`
}

// StorageConfig controls where amm persists data.
type StorageConfig struct {
	DBPath      string `json:"db_path"`
	Backend     string `json:"backend"`
	PostgresDSN string `json:"postgres_dsn"`
}

// RetrievalConfig tunes recall behavior.
type RetrievalConfig struct {
	DefaultLimit       int   `json:"default_limit"`
	AmbientLimit       int   `json:"ambient_limit"`
	EnableSemantic     bool  `json:"enable_semantic"`
	EnableExplain      bool  `json:"enable_explain"`
	EntityHubThreshold int64 `json:"entity_hub_threshold"` // link count before hub dampening kicks in (default 10)
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
			DBPath:      filepath.Join(home, ".amm", "amm.db"),
			Backend:     "sqlite",
			PostgresDSN: "",
		},
		Retrieval: RetrievalConfig{
			DefaultLimit:       10,
			AmbientLimit:       5,
			EnableSemantic:     false,
			EnableExplain:      true,
			EntityHubThreshold: defaultEntityHubThreshold,
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
		Compression: CompressionConfig{
			EscalationDeterministicMaxChars: defaultEscalationDeterministicMaxChars,
		},
		Summarizer: SummarizerConfig{
			ReprocessBatchSize:              defaultReprocessBatchSize,
			ReflectBatchSize:                defaultReflectBatchSize,
			ReflectLLMBatchSize:             defaultReflectLLMBatchSize,
			LifecycleReviewBatchSize:        defaultLifecycleReviewBatchSize,
			CompressChunkSize:               defaultCompressChunkSize,
			CompressMaxEvents:               defaultCompressMaxEvents,
			CompressBatchSize:               defaultCompressBatchSize,
			TopicBatchSize:                  defaultTopicBatchSize,
			EmbeddingBatchSize:              defaultEmbeddingBatchSize,
			CrossProjectSimilarityThreshold: defaultCrossProjectSimilarity,
			SessionIdleTimeoutMinutes:       defaultSessionIdleTimeoutMinutes,
			SummarizerContextWindow:         defaultSummarizerContextWindow,
		},
		Embeddings: EmbeddingsConfig{
			Enabled: false,
		},
		IntakeQuality: IntakeQualityConfig{
			MinConfidenceForCreation: defaultMinConfidenceForCreation,
			MinImportanceForCreation: defaultMinImportanceForCreation,
		},
		MaxExpandDepth: defaultMaxExpandDepth,
		HTTP: HTTPConfig{
			Addr: ":8080",
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
		// Detect explicit "enabled": false in embeddings section.
		if !cfg.Embeddings.Enabled {
			var raw map[string]json.RawMessage
			if json.Unmarshal(data, &raw) == nil {
				if embRaw, ok := raw["embeddings"]; ok {
					var embFields map[string]json.RawMessage
					if json.Unmarshal(embRaw, &embFields) == nil {
						if _, hasEnabled := embFields["enabled"]; hasEnabled {
							cfg.Embeddings.ExplicitlyDisabled = true
						}
					}
				}
			}
		}
		applyConfigDefaults(&cfg)
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
	lineNum := 0
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		lineNum++
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
			slog.Warn("ignoring malformed config line", "line_number", lineNum)
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
		case "storage.backend":
			cfg.Storage.Backend = val
		case "storage.postgres_dsn":
			cfg.Storage.PostgresDSN = val
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
		case "retrieval.entity_hub_threshold":
			if n, err := strconv.ParseInt(val, 10, 64); err == nil && n > 0 {
				cfg.Retrieval.EntityHubThreshold = n
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
		case "compression.escalation_deterministic_max_chars":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Compression.EscalationDeterministicMaxChars = n
			}
		case "summarizer.endpoint":
			cfg.Summarizer.Endpoint = val
		case "summarizer.api_key":
			cfg.Summarizer.APIKey = val
		case "summarizer.model":
			cfg.Summarizer.Model = val
		case "summarizer.reprocess_batch_size":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Summarizer.ReprocessBatchSize = n
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
		case "summarizer.compress_chunk_size":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Summarizer.CompressChunkSize = n
			}
		case "summarizer.compress_max_events":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Summarizer.CompressMaxEvents = n
			}
		case "summarizer.compress_batch_size":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Summarizer.CompressBatchSize = n
			}
		case "summarizer.topic_batch_size":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Summarizer.TopicBatchSize = n
			}
		case "summarizer.embedding_batch_size":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Summarizer.EmbeddingBatchSize = n
			}
		case "summarizer.cross_project_similarity_threshold":
			if f, err := strconv.ParseFloat(val, 64); err == nil && f > 0 {
				cfg.Summarizer.CrossProjectSimilarityThreshold = f
			}
		case "embeddings.enabled":
			if b, err := strconv.ParseBool(val); err == nil {
				cfg.Embeddings.Enabled = b
				if !b {
					cfg.Embeddings.ExplicitlyDisabled = true
				}
			}
		case "embeddings.provider":
			cfg.Embeddings.Provider = val
		case "embeddings.endpoint":
			cfg.Embeddings.Endpoint = val
		case "embeddings.api_key":
			cfg.Embeddings.APIKey = val
		case "embeddings.model":
			cfg.Embeddings.Model = val
		case "intake_quality.min_confidence_for_creation":
			if f, err := strconv.ParseFloat(val, 64); err == nil && f >= 0 && f <= 1 {
				cfg.IntakeQuality.MinConfidenceForCreation = f
			}
		case "intake_quality.min_importance_for_creation":
			if f, err := strconv.ParseFloat(val, 64); err == nil && f >= 0 && f <= 1 {
				cfg.IntakeQuality.MinImportanceForCreation = f
			}
		case "max_expand_depth":
			if n, err := strconv.Atoi(val); err == nil && n >= -1 {
				cfg.MaxExpandDepth = n
			}
		case "http.addr":
			cfg.HTTP.Addr = val
		case "http.cors_origins":
			cfg.HTTP.CORSOrigins = val
		case "api.url":
			cfg.API.URL = val
		case "api.key":
			cfg.API.Key = val
		}
	}
	applyConfigDefaults(cfg)
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
	applyConfigDefaults(&cfg)
	return cfg
}

// ConfigFromEnv overrides config fields with environment variables.
// Supported variables:
//
//	AMM_DB_PATH           -> Storage.DBPath
//	AMM_STORAGE_BACKEND   -> Storage.Backend
//	AMM_POSTGRES_DSN      -> Storage.PostgresDSN
//	AMM_DEFAULT_LIMIT     -> Retrieval.DefaultLimit
//	AMM_AMBIENT_LIMIT     -> Retrieval.AmbientLimit
//	AMM_ENABLE_SEMANTIC   -> Retrieval.EnableSemantic (true/false)
//	AMM_ENABLE_EXPLAIN    -> Retrieval.EnableExplain  (true/false)
//	AMM_DEFAULT_PRIVACY   -> Privacy.DefaultPrivacy
//	AMM_AUTO_REFLECT      -> Maintenance.AutoReflect  (true/false)
//	AMM_AUTO_COMPRESS     -> Maintenance.AutoCompress  (true/false)
//	AMM_AUTO_CONSOLIDATE  -> Maintenance.AutoConsolidate (true/false)
//	AMM_AUTO_DETECT_CONTRADICTIONS -> Maintenance.AutoDetectContradictions (true/false)
//	AMM_ESCALATION_DETERMINISTIC_MAX_CHARS -> Compression.EscalationDeterministicMaxChars
//	AMM_SUMMARIZER_ENDPOINT -> Summarizer.Endpoint
//	AMM_SUMMARIZER_API_KEY -> Summarizer.APIKey
//	AMM_SUMMARIZER_MODEL -> Summarizer.Model
//	AMM_REPROCESS_BATCH_SIZE -> Summarizer.ReprocessBatchSize
//	AMM_COMPRESS_CHUNK_SIZE -> Summarizer.CompressChunkSize
//	AMM_COMPRESS_MAX_EVENTS -> Summarizer.CompressMaxEvents
//	AMM_COMPRESS_BATCH_SIZE -> Summarizer.CompressBatchSize
//	AMM_TOPIC_BATCH_SIZE -> Summarizer.TopicBatchSize
//	AMM_SESSION_IDLE_TIMEOUT_MINUTES -> Summarizer.SessionIdleTimeoutMinutes
//	AMM_SUMMARIZER_CONTEXT_WINDOW -> Summarizer.SummarizerContextWindow
//	AMM_EMBEDDING_BATCH_SIZE -> Summarizer.EmbeddingBatchSize
//	AMM_CROSS_PROJECT_SIMILARITY_THRESHOLD -> Summarizer.CrossProjectSimilarityThreshold
//	AMM_EMBEDDINGS_ENABLED -> Embeddings.Enabled (true/false)
//	AMM_EMBEDDINGS_PROVIDER -> Embeddings.Provider
//	AMM_EMBEDDINGS_ENDPOINT -> Embeddings.Endpoint
//	AMM_EMBEDDINGS_API_KEY -> Embeddings.APIKey
//	AMM_EMBEDDINGS_MODEL -> Embeddings.Model
//	AMM_MIN_CONFIDENCE_FOR_CREATION -> IntakeQuality.MinConfidenceForCreation (0.0-1.0)
//	AMM_MIN_IMPORTANCE_FOR_CREATION -> IntakeQuality.MinImportanceForCreation (0.0-1.0)
//	AMM_HTTP_ADDR -> HTTP.Addr
//	AMM_HTTP_CORS_ORIGINS -> HTTP.CORSOrigins
//	AMM_API_URL -> API.URL
//	AMM_API_KEY -> API.Key
func ConfigFromEnv(base Config) Config {
	if v := os.Getenv("AMM_DB_PATH"); v != "" {
		base.Storage.DBPath = v
	}
	if v := os.Getenv("AMM_STORAGE_BACKEND"); v != "" {
		base.Storage.Backend = strings.ToLower(strings.TrimSpace(v))
	}
	if strings.TrimSpace(base.Storage.Backend) == "" {
		base.Storage.Backend = "sqlite"
	}
	if v := os.Getenv("AMM_POSTGRES_DSN"); v != "" {
		base.Storage.PostgresDSN = v
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
	if v := os.Getenv("AMM_ESCALATION_DETERMINISTIC_MAX_CHARS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Compression.EscalationDeterministicMaxChars = n
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
	if v := os.Getenv("AMM_REPROCESS_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Summarizer.ReprocessBatchSize = n
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
	if v := os.Getenv("AMM_COMPRESS_CHUNK_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Summarizer.CompressChunkSize = n
		}
	}
	if v := os.Getenv("AMM_COMPRESS_MAX_EVENTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Summarizer.CompressMaxEvents = n
		}
	}
	if v := os.Getenv("AMM_COMPRESS_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Summarizer.CompressBatchSize = n
		}
	}
	if v := os.Getenv("AMM_TOPIC_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Summarizer.TopicBatchSize = n
		}
	}
	if v := os.Getenv("AMM_EMBEDDING_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Summarizer.EmbeddingBatchSize = n
		}
	}
	if v := os.Getenv("AMM_CROSS_PROJECT_SIMILARITY_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			base.Summarizer.CrossProjectSimilarityThreshold = f
		}
	}
	if v := os.Getenv("AMM_SESSION_IDLE_TIMEOUT_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			base.Summarizer.SessionIdleTimeoutMinutes = n
		}
	}
	if v := os.Getenv("AMM_SUMMARIZER_CONTEXT_WINDOW"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			base.Summarizer.SummarizerContextWindow = n
		}
	}
	if v := os.Getenv("AMM_EMBEDDINGS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			base.Embeddings.Enabled = b
			if !b {
				base.Embeddings.ExplicitlyDisabled = true
			}
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
	if v := os.Getenv("AMM_ENTITY_HUB_THRESHOLD"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			base.Retrieval.EntityHubThreshold = n
		}
	}
	if v := os.Getenv("AMM_MIN_CONFIDENCE_FOR_CREATION"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			base.IntakeQuality.MinConfidenceForCreation = f
		}
	}
	if v := os.Getenv("AMM_MIN_IMPORTANCE_FOR_CREATION"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			base.IntakeQuality.MinImportanceForCreation = f
		}
	}
	if v := os.Getenv("AMM_MAX_EXPAND_DEPTH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= -1 {
			base.MaxExpandDepth = n
		}
	}
	if v := os.Getenv("AMM_HTTP_ADDR"); v != "" {
		base.HTTP.Addr = v
	}
	if v := os.Getenv("AMM_HTTP_CORS_ORIGINS"); v != "" {
		base.HTTP.CORSOrigins = v
	}
	if v := os.Getenv("AMM_API_URL"); v != "" {
		base.API.URL = v
	}
	if v := os.Getenv("AMM_API_KEY"); v != "" {
		base.API.Key = v
	}
	applyConfigDefaults(&base)
	return base
}

func applyConfigDefaults(cfg *Config) {
	if strings.TrimSpace(cfg.HTTP.Addr) == "" {
		cfg.HTTP.Addr = ":8080"
	}
	if cfg.Compression.EscalationDeterministicMaxChars <= 0 {
		cfg.Compression.EscalationDeterministicMaxChars = defaultEscalationDeterministicMaxChars
	}
	if cfg.MaxExpandDepth < -1 {
		cfg.MaxExpandDepth = defaultMaxExpandDepth
	}
}
