package k8sforward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/cmd/portforward"
	"k8s.io/kubectl/pkg/cmd/util"
)

type Settings struct {
	// ContextName (required) is the k8s context to use.
	ContextName string
	// AppName  (required) selects for pods with the label app='AppName'.
	// If more than one pod is found, the first pod encountered is used.
	AppName string
	// LocalAddress (required) is the local address to port-forward to.
	LocalAddress string
	// RemotePort (required) is the port on the pod to port-forward from.
	RemotePort string
	// VersionName  (optional). If given this sub-selects for pods with the label version='VersionName'.
	// If more than one pod is found, the first pod encountered is used.
	VersionName string
	// KubeconfigPath (optional). This overrides the path to the kubeconfig file from the default value of $HOME/.kube/config.
	KubeconfigPath string
	// ReadyChannel (optional). If ReadyChannel is specified, the commencement of port-forwarding can be detected by receiving from it.
	ReadyChannel chan struct{}
	// CancelFn (optional). If CancelFn is specified, it will be called upon any error except context.Canceled.
	CancelFn context.CancelFunc
	// Out is the data stream for output (optional). Defaults to os.Stdout.
	Out io.Writer
	// ErrOut is the data stream for error output (optional). Defaults to os.Stderr.
	ErrOut io.Writer

	localHost string
	localPort string
}

func (s *Settings) Validate() error {
	if err := validateNonEmptyString("k8s context name", s.ContextName); err != nil {
		return err
	}
	if err := validateNonEmptyString("k8s app name", s.AppName); err != nil {
		return err
	}
	addressParts, err := validateLocalAddress(s.LocalAddress)
	if err != nil {
		return err
	}
	if err := validateTCPPort("remote TCP port", s.RemotePort); err != nil {
		return err
	}
	s.localHost = addressParts[0]
	s.localPort = addressParts[1]
	return nil
}

// Init initiates port-forwarding with the given Go context `ctx`.
func Init(ctx context.Context, settings *Settings) error {
	if err := run(ctx, settings); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if settings.CancelFn != nil {
			settings.CancelFn()
		}
		return err
	}
	return nil
}

func run(ctx context.Context, settings *Settings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	if settings.KubeconfigPath == "" {
		homeDir, ok := os.LookupEnv("HOME")
		if !ok {
			return fmt.Errorf("cannot resolve home directory")
		}
		settings.KubeconfigPath = filepath.Join(homeDir, ".kube", "config")
	}
	if settings.Out == nil {
		settings.Out = os.Stdout
	}
	if settings.ErrOut == nil {
		settings.ErrOut = os.Stderr
	}

	apiConfig, err := clientcmd.LoadFromFile(settings.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("error loading k8s config from %s: %w", settings.KubeconfigPath, err)
	}

	k8sCtx, ok := apiConfig.Contexts[settings.ContextName]
	if !ok {
		return fmt.Errorf("unknown context '%s'", settings.ContextName)
	}

	// Create a ClientConfig from the apiConfig
	clientConfig := clientcmd.NewDefaultClientConfig(*apiConfig, &clientcmd.ConfigOverrides{
		CurrentContext: settings.ContextName,
	})

	// Get the *rest.Settings from the ClientConfig
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("error creating k8s client REST config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("error creating k8s clientset: %w", err)
	}

	podClient := clientset.CoreV1()
	labelSelector := fmt.Sprintf("app=%s", settings.AppName)
	missingErr := fmt.Errorf("no k8s pods found for app '%s' in '%s' context", settings.AppName, settings.ContextName)
	if settings.VersionName != "" {
		labelSelector = fmt.Sprintf("app=%s,version=%s", settings.AppName, settings.VersionName)
		missingErr = fmt.Errorf("no k8s pods found for app '%s' version '%s' in '%s' context", settings.AppName, settings.VersionName, settings.ContextName)
	}
	pods, err := podClient.Pods(k8sCtx.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("error listing k8s pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return missingErr
	}

	var podName string
	for _, pod := range pods.Items {
		// Just pick the first pod matching the label selector
		podName = pod.Name
		break
	}

	// Copy the K8s CLI code for creating a factory
	kubeConfigFlags := genericclioptions.NewConfigFlags(true)
	matchVersionKubeConfigFlags := util.NewMatchVersionFlags(kubeConfigFlags)
	factory := util.NewFactory(matchVersionKubeConfigFlags)

	portForwardOptions := portforward.NewDefaultPortForwardOptions(
		genericiooptions.IOStreams{
			In:     os.Stdin,
			Out:    settings.Out,
			ErrOut: settings.ErrOut,
		},
	)
	portForwardOptions.RESTClient, err = factory.RESTClient()
	if err != nil {
		return fmt.Errorf("error configuring k8s REST client: %w", err)
	}

	portForwardOptions.PodClient = clientset.CoreV1()
	portForwardOptions.Namespace = k8sCtx.Namespace
	portForwardOptions.PodName = podName
	portForwardOptions.Address = []string{settings.localHost}
	portForwardOptions.Ports = []string{fmt.Sprintf("%s:%s", settings.localPort, settings.RemotePort)}
	portForwardOptions.Config = restConfig

	portForwardOptions.StopChannel = make(chan struct{}, 1)

	if settings.ReadyChannel != nil {
		portForwardOptions.ReadyChannel = settings.ReadyChannel
	} else {
		portForwardOptions.ReadyChannel = make(chan struct{})
	}

	if err = portForwardOptions.Validate(); err != nil {
		return fmt.Errorf("error validating the k8s portforward options: %w", err)
	}

	_, err = fmt.Fprintf(settings.Out, "Starting k8s port forward from %s to %s:%s\n", settings.LocalAddress, podName, settings.RemotePort)
	if err != nil {
		return fmt.Errorf("error writing to output: %w", err)
	}
	if err = portForwardOptions.RunPortForwardContext(ctx); err != nil {
		return fmt.Errorf("error port forwarding to k8s pod: %w", err)
	}

	return nil
}
