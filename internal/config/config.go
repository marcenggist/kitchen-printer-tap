package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the kitchen printer tap daemon.
type Config struct {
	DeviceID  string `yaml:"device_id"`
	SiteID    string `yaml:"site_id"`
	Interface string `yaml:"interface"`

	Capture CaptureConfig `yaml:"capture"`
	Storage StorageConfig `yaml:"storage"`
	Upload  UploadConfig  `yaml:"upload"`
	Health  HealthConfig  `yaml:"health"`
	Metrics MetricsConfig `yaml:"metrics"`
}

// CaptureConfig holds packet capture settings.
type CaptureConfig struct {
	Port9100Enabled bool          `yaml:"port_9100_enabled"`
	Port515Enabled  bool          `yaml:"port_515_enabled"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	SnapLen         int           `yaml:"snap_len"`
	Promiscuous     bool          `yaml:"promiscuous"`
	BufferSizeMB    int           `yaml:"buffer_size_mb"`
}

// StorageConfig holds local storage settings.
type StorageConfig struct {
	BasePath         string `yaml:"base_path"`
	MinFreeMB        int    `yaml:"min_free_mb"`
	RetentionDays    int    `yaml:"retention_days"`
	ReprintWindowSec int    `yaml:"reprint_window_sec"`
}

// UploadConfig holds webhook upload settings.
type UploadConfig struct {
	Enabled      bool          `yaml:"enabled"`
	WebhookURL   string        `yaml:"webhook_url"`
	AuthToken    string        `yaml:"auth_token"`
	MaxRetries   int           `yaml:"max_retries"`
	RetryBackoff time.Duration `yaml:"retry_backoff"`
	Timeout      time.Duration `yaml:"timeout"`
	BatchSize    int           `yaml:"batch_size"`
}

// HealthConfig holds health endpoint settings.
type HealthConfig struct {
	Enabled bool   `yaml:"enabled"`
	Address string `yaml:"address"`
}

// MetricsConfig holds metrics logging settings.
type MetricsConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		DeviceID:  "kptap-001",
		SiteID:    "site-001",
		Interface: "br0",
		Capture: CaptureConfig{
			Port9100Enabled: true,
			Port515Enabled:  false,
			IdleTimeout:     800 * time.Millisecond,
			SnapLen:         65535,
			Promiscuous:     true,
			BufferSizeMB:    8,
		},
		Storage: StorageConfig{
			BasePath:         "/var/lib/kitchen-printer-tap",
			MinFreeMB:        100,
			RetentionDays:    30,
			ReprintWindowSec: 300,
		},
		Upload: UploadConfig{
			Enabled:      false,
			WebhookURL:   "",
			AuthToken:    "",
			MaxRetries:   3,
			RetryBackoff: 5 * time.Second,
			Timeout:      30 * time.Second,
			BatchSize:    10,
		},
		Health: HealthConfig{
			Enabled: true,
			Address: "127.0.0.1:8088",
		},
		Metrics: MetricsConfig{
			Enabled:  true,
			Interval: 60 * time.Second,
		},
	}
}

// Load reads configuration from a YAML file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// Validate checks configuration for errors.
func (c *Config) Validate() error {
	if c.DeviceID == "" {
		return fmt.Errorf("device_id is required")
	}
	if c.SiteID == "" {
		return fmt.Errorf("site_id is required")
	}
	if c.Interface == "" {
		return fmt.Errorf("interface is required")
	}
	if !c.Capture.Port9100Enabled && !c.Capture.Port515Enabled {
		return fmt.Errorf("at least one capture port must be enabled")
	}
	if c.Capture.IdleTimeout < 100*time.Millisecond {
		return fmt.Errorf("idle_timeout must be at least 100ms")
	}
	if c.Storage.BasePath == "" {
		return fmt.Errorf("storage base_path is required")
	}
	if c.Upload.Enabled && c.Upload.WebhookURL == "" {
		return fmt.Errorf("webhook_url is required when upload is enabled")
	}
	return nil
}
