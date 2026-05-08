package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen      string            `yaml:"listen"`
	API         string            `yaml:"api"`
	Subscribe   []SubscribeConfig `yaml:"subscribe"`
	HealthCheck HealthCheckConfig `yaml:"health_check"`
}

type SubscribeConfig struct {
	URL      string `yaml:"url"`
	Interval int    `yaml:"interval"` // seconds
	Name     string `yaml:"name"`
}

type HealthCheckConfig struct {
	URL         string `yaml:"url"`
	Interval    int    `yaml:"interval"` // seconds
	Timeout     int    `yaml:"timeout"`  // seconds
	MaxFailures int    `yaml:"max_failures"`
}

func Default() *Config {
	return &Config{
		Listen: ":7890",
		API:    ":9090",
		HealthCheck: HealthCheckConfig{
			URL:         "https://www.gstatic.com/generate_204",
			Interval:    300,
			Timeout:     5,
			MaxFailures: 3,
		},
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
