package fw

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds the application configuration.
// It supports arbitrary nested YAML that modules can decode into their own structs.
type Config struct {
	// App holds top-level application settings.
	App AppConfig `yaml:"app"`

	// Database holds database connection settings.
	Database DatabaseConfig `yaml:"database"`

	// Log holds logging settings.
	Log LogConfig `yaml:"log"`

	// Modules holds per-module configuration as raw YAML.
	// Each module can decode its section into its own config struct.
	Modules map[string]yaml.Node `yaml:"modules"`
}

// AppConfig holds top-level application settings.
type AppConfig struct {
	Name string `yaml:"name"`
	Addr string `yaml:"addr"`
}

// DatabaseConfig holds database connection settings.
type DatabaseConfig struct {
	DSN             string `yaml:"dsn"`
	MaxOpenConns    int    `yaml:"max_open_conns"`
	MaxIdleConns    int    `yaml:"max_idle_conns"`
	ConnMaxLifetime string `yaml:"conn_max_lifetime"`
}

// LogConfig holds logging configuration.
type LogConfig struct {
	Level string `yaml:"level"`
}

// LoadConfig reads and parses a YAML config file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fw: failed to read config file %q: %w", path, err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("fw: failed to parse config file %q: %w", path, err)
	}

	return cfg, nil
}

// ModuleConfig decodes a module's raw YAML config into the given struct.
// Returns false if no config exists for the given module name.
func (c *Config) ModuleConfig(name string, dest any) (bool, error) {
	node, ok := c.Modules[name]
	if !ok {
		return false, nil
	}

	if err := node.Decode(dest); err != nil {
		return false, fmt.Errorf("fw: failed to decode config for module %q: %w", name, err)
	}

	return true, nil
}
