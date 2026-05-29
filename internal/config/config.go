// Package config handles startup configuration file loading.
//
// The server reads config.local.json first; if absent, falls back to config.json.
package config

import (
	"encoding/json"
	"os"
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

// Load reads config.local.json first, falling back to config.json.
// Returns (nil, nil) when neither file is found.
func Load() (*Config, error) {
	for _, name := range []string{"config.local.json", "config.json"} {
		data, err := os.ReadFile(name)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
		return &cfg, nil
	}
	return nil, nil
}
