// Package config handles startup configuration file loading with optional
// environment variable overrides.
//
// The server reads config.local.json first; if absent, falls back to config.json.
// After file loading, environment variables with the BROSDK_ prefix override
// individual fields. Priority: env var > config file > hardcoded default.
package config

import (
	"encoding/json"
	"os"
	"strconv"
)

// Config mirrors the JSON config file fields used for sdk_init.
type Config struct {
	ApiKey    string `json:"apiKey"`
	UserSig   string `json:"userSig"`
	WorkDir   string `json:"workDir"`
	Port      int    `json:"port"`
	SdkApiURL string `json:"sdkApiUrl"`
	Debug     bool   `json:"debug"`
}

// Load reads config.local.json first, falling back to config.json, then
// applies environment variable overrides (BROSDK_ prefix).
// Returns a non-nil *Config even when no file is found, as long as
// environment variables provide configuration. Returns (nil, nil) only
// when neither a file nor relevant env vars are present.
func Load() (*Config, error) {
	var cfg Config
	fileLoaded := false

	for _, name := range []string{"config.local.json", "config.json"} {
		data, err := os.ReadFile(name)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
		fileLoaded = true
		break
	}

	// Apply environment variable overrides.
	envApplied := applyEnvOverrides(&cfg)

	if !fileLoaded && !envApplied {
		return nil, nil
	}
	return &cfg, nil
}

// applyEnvOverrides checks for BROSDK_* environment variables and overrides
// the corresponding Config fields. Returns true if at least one env var was set.
func applyEnvOverrides(cfg *Config) bool {
	applied := false

	if v, ok := os.LookupEnv("BROSDK_API_KEY"); ok && v != "" {
		cfg.ApiKey = v
		applied = true
	}
	if v, ok := os.LookupEnv("BROSDK_USER_SIG"); ok && v != "" {
		cfg.UserSig = v
		applied = true
	}
	if v, ok := os.LookupEnv("BROSDK_WORK_DIR"); ok && v != "" {
		cfg.WorkDir = v
		applied = true
	}
	if v, ok := os.LookupEnv("BROSDK_PORT"); ok && v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			cfg.Port = p
			applied = true
		}
	}
	if v, ok := os.LookupEnv("BROSDK_SDK_API_URL"); ok && v != "" {
		cfg.SdkApiURL = v
		applied = true
	}
	if v, ok := os.LookupEnv("BROSDK_DEBUG"); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Debug = b
			applied = true
		}
	}
	return applied
}
