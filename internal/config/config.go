package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

var cfg *Config

// Load reads config from file and environment
func Load(configPath string) (*Config, error) {
	v := viper.New()

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		// Look for config in ~/.ori/config.json.
		home, err := os.UserHomeDir()
		if err == nil {
			v.AddConfigPath(home + "/.ori")
			configPath = DefaultConfigPath(home)
		}
		v.SetConfigName("config")
		v.SetConfigType("json")
	}

	// Environment variable overrides
	v.SetEnvPrefix("ORI")
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
		// Config file not found, use defaults
		cfg = &Config{SourcePath: configPath}
		return cfg, nil
	}

	cfg = &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	cfg.SourcePath = configPath

	return cfg, nil
}

// Get returns the loaded config
func Get() *Config {
	return cfg
}
