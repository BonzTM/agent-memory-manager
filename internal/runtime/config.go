package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

// Config holds all runtime configuration for AMM.
// Matches blueprint section 15.
type Config struct {
	Storage     StorageConfig     `json:"storage"`
	Retrieval   RetrievalConfig   `json:"retrieval"`
	Privacy     PrivacyConfig     `json:"privacy"`
	Maintenance MaintenanceConfig `json:"maintenance"`
}

// StorageConfig controls where AMM persists data.
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
	AutoReflect             bool `json:"auto_reflect"`
	AutoCompress            bool `json:"auto_compress"`
	AutoConsolidate         bool `json:"auto_consolidate"`
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
			AutoReflect:             true,
			AutoCompress:            true,
			AutoConsolidate:         true,
			AutoDetectContradictions: true,
		},
	}
}

// LoadConfig reads a JSON config file and merges it with defaults.
// If the file does not exist, defaults are returned without error.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig(), err
	}
	return cfg, nil
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
	return base
}
