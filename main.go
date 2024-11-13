package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/fioncat/kubesh/config"
	"github.com/fioncat/kubesh/nodeshell"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	Version   string = "N/A"
	Commit    string = "N/A"
	BuildDate string = "N/A"
)

func main() {
	cmd := newCommand()

	err := cmd.Execute()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func newCommand() *cobra.Command {
	var buildInfo bool
	var opts commandOptions

	cmd := &cobra.Command{
		Use:   "kubesh [NODE]",
		Short: "Login to a node in k8s cluster",

		Args: cobra.MaximumNArgs(1),

		SilenceErrors: true,
		SilenceUsage:  true,

		CompletionOptions: cobra.CompletionOptions{
			HiddenDefaultCmd: true,
		},

		Version: Version,

		RunE: func(cmd *cobra.Command, args []string) error {
			if buildInfo {
				fmt.Printf("Version: %s\n", Version)
				fmt.Printf("Commit: %s\n", Commit)
				fmt.Printf("Build Date: %s\n", BuildDate)
				return nil
			}

			if len(args) > 0 {
				opts.nodeName = args[0]
			}

			return opts.run()
		},
	}

	cmd.Flags().BoolVarP(&buildInfo, "build-info", "", false, "show build info")

	cmd.Flags().StringVarP(&opts.configPath, "config", "c", "", "kubesh config file path (default ~/.config/kubesh.yaml)")
	cmd.Flags().StringVar(&opts.kubeConfigPath, "kubeconfig", "", "kubeconfig file path (default from env $KUBECONFIG and ~/.kube/config)")

	cmd.Flags().BoolVarP(&opts.keepPod, "keep", "k", false, "don't delete shell pod after exit")
	cmd.Flags().BoolVarP(&opts.killPod, "kill", "K", false, "kill shell pod")
	cmd.Flags().BoolVarP(&opts.Insecure, "insecure", "i", false, "allow insecure connection to cluster")

	return cmd
}

type commandOptions struct {
	nodeName string

	keepPod bool
	killPod bool

	configPath     string
	kubeConfigPath string

	Insecure bool

	kubeClient *kubernetes.Clientset
	kubeConfig *rest.Config
}

func (o *commandOptions) run() error {
	err := o.initKubeClient()
	if err != nil {
		return err
	}

	err = o.ensureNode()
	if err != nil {
		return err
	}

	config, err := config.Load(o.configPath)
	if err != nil {
		return err
	}

	nodeShell, err := nodeshell.New(o.nodeName, config, o.kubeConfig, o.kubeClient)
	if err != nil {
		return err
	}
	if o.killPod {
		return nodeShell.Close()
	}
	if !o.keepPod {
		defer nodeShell.RetryClose()
	}

	return nodeShell.Run()
}

func (o *commandOptions) ensureNode() error {
	if o.nodeName != "" {
		return nil
	}

	ctx := context.Background()
	nodeList, err := o.kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list nodes error: %w", err)
	}

	nodes := make([]string, 0, len(nodeList.Items))
	for _, item := range nodeList.Items {
		nodes = append(nodes, item.Name)
	}

	if len(nodes) == 0 {
		return errors.New("no node in cluster")
	}

	prompt := promptui.Select{
		Label:     "Select Node",
		Items:     nodes,
		Size:      20,
		IsVimMode: true,
	}
	_, result, err := prompt.Run()
	if err != nil {
		return fmt.Errorf("prompt error: %w", err)
	}

	o.nodeName = result
	return nil
}

func (o *commandOptions) initKubeClient() error {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = o.kubeConfigPath

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, nil).ClientConfig()
	if err != nil {
		return fmt.Errorf("read kube config error: %w", err)
	}
	if o.Insecure {
		cfg.Insecure = true
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("create kube client error: %w", err)
	}

	o.kubeConfig = cfg
	o.kubeClient = client
	return nil
}
