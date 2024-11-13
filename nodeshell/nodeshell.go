package nodeshell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fioncat/kubesh/config"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/util/term"
	"k8s.io/utils/ptr"
)

const (
	containerName = "node-shell"

	checkPodStatusInterval = time.Second
	checkPodStatusTimeout  = time.Minute

	closeMaxRetry  = 5
	closeRetryTime = time.Second * 3
)

type NodeShell struct {
	node string

	podName string

	config *config.Config

	kubeConfig *rest.Config
	kubeClient *kubernetes.Clientset
}

func New(node string, config *config.Config, kubeConfig *rest.Config, kubeClient *kubernetes.Clientset) (*NodeShell, error) {
	podName := strings.ReplaceAll(config.PodName, "{node}", node)
	ns := &NodeShell{
		node:       node,
		podName:    podName,
		config:     config,
		kubeConfig: kubeConfig,
		kubeClient: kubeClient,
	}
	err := ns.start()
	if err != nil {
		return nil, err
	}

	return ns, nil
}

func (n *NodeShell) start() error {
	ctx := context.Background()
	_, err := n.kubeClient.CoreV1().Nodes().Get(ctx, n.node, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		return fmt.Errorf("node %s not found in cluster", n.node)
	}
	if err != nil {
		return fmt.Errorf("get node %s error: %w", n.node, err)
	}

	_, err = n.kubeClient.CoreV1().Namespaces().Get(ctx, n.config.PodNamespace, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		return fmt.Errorf("namespace %s not found in cluster, please create it first", n.config.PodNamespace)
	}
	if err != nil {
		return fmt.Errorf("get namespace %s error: %w", n.config.PodNamespace, err)
	}

	// Check if the node shell pod exists, and create it if it doesn't exist
	pod, err := n.kubeClient.CoreV1().Pods(n.config.PodNamespace).Get(ctx, n.podName, metav1.GetOptions{})
	if err == nil {
		// If the pod exists and is already in the "Running" state, consider the node shell to be ready
		if pod.Status.Phase == v1.PodRunning {
			return nil
		}
	} else {
		if !kerrors.IsNotFound(err) {
			return fmt.Errorf("get node-shell pod error: %w", err)
		}
		pod = n.buildPod()
		_, err = n.kubeClient.CoreV1().Pods(n.config.PodNamespace).Create(ctx, pod, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create node-shell pod error: %w", err)
		}
	}

	// Wait for the pod to start up, and wait for no more than the timeout period
	checkStatusTk := time.NewTicker(checkPodStatusInterval)
	checkStatusTimeout := time.NewTimer(checkPodStatusTimeout)

	for {
		select {
		case <-checkStatusTk.C:
			pod, err := n.kubeClient.CoreV1().Pods(n.config.PodNamespace).Get(ctx, n.podName, metav1.GetOptions{})
			if err != nil {
				if !kerrors.IsNotFound(err) {
					return fmt.Errorf("get node-shell pod error: %v", err)
				}
				continue
			}

			if pod.Status.Phase == v1.PodRunning {
				return nil
			}

		case <-checkStatusTimeout.C:
			return errors.New("timeout to wait node-shell pod to running")
		}
	}
}

func (n *NodeShell) Run() error {
	t := term.TTY{
		In:  os.Stdin,
		Out: os.Stdout,
		Raw: true,
	}
	sizeQueue := t.MonitorSize(t.GetSize())

	if !t.IsTerminalOut() {
		return errors.New("unable to setup tty, input is not a terminal")
	}

	req := n.kubeClient.CoreV1().RESTClient().Post().Resource("pods").Name(n.podName).Namespace(n.config.PodNamespace).SubResource("exec")
	opts := &v1.PodExecOptions{
		Container: containerName,
		Command:   n.config.ShellCommand,
		Stdin:     true,
		Stdout:    true,
		// set Stderr to false because both stdout and stderr go over t.Out when tty is true
		Stderr: false,
		TTY:    true,
	}
	req.VersionedParams(opts, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(n.kubeConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("create executor error: %w", err)
	}

	streamOpts := remotecommand.StreamOptions{
		Stdin:             t.In,
		Stdout:            t.Out,
		Stderr:            nil,
		Tty:               true,
		TerminalSizeQueue: sizeQueue,
	}

	return t.Safe(func() error {
		err = exec.StreamWithContext(context.Background(), streamOpts)
		if err != nil {
			return fmt.Errorf("exec shell command error: %w", err)
		}

		return nil
	})
}

func (n *NodeShell) buildPod() *v1.Pod {
	args := []string{"-t", "1", "-m", "-u", "-i", "-n"}
	args = append(args, n.config.PauseCommand...)
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      n.podName,
			Namespace: n.config.PodNamespace,
			Labels: map[string]string{
				"app":   "node-shell",
				"owner": "kubesh",
			},
		},
		Spec: v1.PodSpec{
			NodeName:    n.node,
			HostNetwork: true,
			HostPID:     true,
			HostIPC:     true,
			Containers: []v1.Container{
				{
					Name:    containerName,
					Image:   n.config.Image,
					Command: []string{"nsenter"},
					Args:    args,
					SecurityContext: &v1.SecurityContext{
						Privileged: ptr.To(true),
					},
				},
			},
		},
	}
}

func (n *NodeShell) Close() error {
	ctx := context.Background()
	err := n.kubeClient.CoreV1().Pods(n.config.PodNamespace).Delete(ctx, n.podName, metav1.DeleteOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("delete node-shell pod error: %v", err)
	}

	return nil
}

func (n *NodeShell) RetryClose() {
	for i := 0; i < closeMaxRetry; i++ {
		err := n.Close()
		if err != nil {
			time.Sleep(closeRetryTime)
			fmt.Printf("WARNING: Failed to close node shell: %v\n", err)
			continue
		}
		return
	}
}
