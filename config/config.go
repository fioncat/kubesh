package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	defaultImage        = "uhub.service.ucloud.cn/library/alpine:latest"
	defaultPodNamespace = "kube-system"
	defaultPodName      = "node-shell-{node}"
)

var (
	defaultPauseCommand = []string{"sleep", "infinity"}
	defaultShellCommand = []string{"bash"}
)

type Config struct {
	Image string `yaml:"image"`

	PauseCommand []string `yaml:"pauseCommand"`
	ShellCommand []string `yaml:"shellCommand"`

	PodNamespace string `yaml:"podNamespace"`
	PodName      string `yaml:"podName"`
}

func Load(configPath string) (*Config, error) {
	if configPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get user home dir: %w", err)
		}
		configPath = filepath.Join(homeDir, ".config", "kubesh.yaml")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}

		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	err = cfg.Validate()
	if err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

func Default() *Config {
	return &Config{
		Image:        defaultImage,
		PauseCommand: defaultPauseCommand,
		ShellCommand: defaultShellCommand,
		PodNamespace: defaultPodNamespace,
		PodName:      defaultPodName,
	}
}

func (c *Config) Validate() error {
	if c.Image == "" {
		c.Image = defaultImage
	}
	// TODO: Validate image name

	if len(c.PauseCommand) == 0 {
		c.PauseCommand = defaultPauseCommand
	}

	if len(c.ShellCommand) == 0 {
		c.ShellCommand = defaultShellCommand
	}

	if c.PodNamespace == "" {
		c.PodNamespace = defaultPodNamespace
	}

	if c.PodName == "" {
		c.PodName = defaultPodName
	}

	return nil
}
