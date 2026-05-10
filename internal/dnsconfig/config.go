package dnsconfig

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Server is one DNS resolver entry from the config file.
type Server struct {
	Name    string `yaml:"name"`
	Address string `yaml:"address"`
}

// Config is the parsed representation of a dns-servers YAML file.
type Config struct {
	Servers []Server `yaml:"servers"`
}

// Load parses a YAML file at path into a Config.
// Each server must have a non-empty address; name defaults to address if omitted.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	for i, s := range cfg.Servers {
		if s.Address == "" {
			return nil, fmt.Errorf("server entry %d is missing an address", i)
		}
		if cfg.Servers[i].Name == "" {
			cfg.Servers[i].Name = s.Address
		}
	}
	if len(cfg.Servers) == 0 {
		return nil, fmt.Errorf("%s contains no server entries", path)
	}
	return &cfg, nil
}
