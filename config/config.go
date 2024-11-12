package config

type Config struct {
	Image string

	PauseCommand []string
	ShellCommand []string

	PodNamespace string
	PodName      string
}

func Load(configPath string) (*Config, error) {
	return &Config{
		Image:        "uhub.service.ucloud.cn/library/alpine:latest",
		PauseCommand: []string{"sleep", "infinity"},
		ShellCommand: []string{"bash"},
		PodNamespace: "kube-system",
		PodName:      "node-shell-{node}",
	}, nil
}
