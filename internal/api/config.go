package api

import (
	"fmt"

	"github.com/Order52/truesense/internal/config"
	"github.com/pelletier/go-toml/v2"
)

// ConfigPaths are the configuration files that are read, system first.
func (a *API) ConfigPaths() []string { return []string{a.SystemConfig, a.UserConfig} }

// ConfigValues is the whole configuration (the files over the defaults) as the nested object
// the settings file has, keyed by the setting names of the file.
func (a *API) ConfigValues() (map[string]any, error) {
	c, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	return configMap(c)
}

// ConfigTOML is the whole configuration as TOML text.
func (a *API) ConfigTOML() (string, error) {
	c, err := a.LoadConfig()
	if err != nil {
		return "", err
	}
	text, err := tomlText(c)
	if err != nil {
		return "", fmt.Errorf("write the configuration: %w", err)
	}
	return text, nil
}

// ValidateConfig lists the problems of the configuration (empty: valid), including a file
// that does not parse, which keeps the program on the defaults.
func (a *API) ValidateConfig() []string {
	c, err := a.LoadConfig()
	if err != nil {
		return []string{err.Error()}
	}
	return c.Validate()
}

func configMap(c *config.Config) (map[string]any, error) {
	b, err := toml.Marshal(c)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	err = toml.Unmarshal(b, &m)
	return m, err
}
