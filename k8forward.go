package k8sforward

import (
	"context"
	"errors"
	"fmt"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/cmd/portforward"
	"os"
	"path/filepath"
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
	validated bool
}

func (s *Settings) Validate() error {
	if s.validated {
		return nil
	}

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
	s.localHost = addressParts[0]
	s.localPort = addressParts[1]

	if err := validateTCPPort("remote TCP port", s.RemotePort); err != nil {
		return err
	}

	if s.KubeconfigPath == "" {
		homeDir, ok := os.LookupEnv("HOME")
		if !ok {
			return fmt.Errorf("cannot resolve home directory")
		}
		s.KubeconfigPath = filepath.Join(homeDir, ".kube", "config")
	}

	if s.Out == nil {
		s.Out = os.Stdout
	}

	if s.ErrOut == nil {
		s.ErrOut = os.Stderr
	}

	s.validated = true

	return nil
}

// Init initiates port-forwarding with the given Go context `ctx`.
func Init(ctx context.Context, s *Settings) error {
	if err := s.run(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if s.CancelFn != nil {
			s.CancelFn()
		}
		return err
	}
	return nil
}

func (s *Settings) run(ctx context.Context) error {
	if err := s.Validate(); err != nil {
		return err
	}

	apiConfig, err := clientcmd.LoadFromFile(s.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("error loading the k8s config from %s: %w", s.KubeconfigPath, err)
	}

	k8sCtx, ok := apiConfig.Contexts[s.ContextName]
	if !ok {
		return fmt.Errorf("unknown k8s context '%s'", s.ContextName)
	}

	clientConfig := clientcmd.NewDefaultClientConfig(*apiConfig, &clientcmd.ConfigOverrides{
		CurrentContext: s.ContextName,
	})

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("error creating the k8s client REST config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("error creating k8s client set: %w", err)
	}

	podClient := clientset.CoreV1()
	labelSelector := fmt.Sprintf("app=%s", s.AppName)
	missingErr := fmt.Errorf("no running pods found for app '%s' in '%s' context", s.AppName, s.ContextName)

	if s.VersionName != "" {
		labelSelector = fmt.Sprintf("app=%s,version=%s", s.AppName, s.VersionName)
		missingErr = fmt.Errorf("no running pods found for app '%s' version '%s' in '%s' context", s.AppName, s.VersionName, s.ContextName)
	}

	pods, err := podClient.Pods(k8sCtx.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return fmt.Errorf("error listing pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return missingErr
	}

	var podName string
	for _, pod := range pods.Items {
		// Just pick the first running pod matching the label selector
		podName = pod.Name
		break
	}

	portForwardOptions := portforward.NewDefaultPortForwardOptions(
		genericiooptions.IOStreams{
			In:     os.Stdin,
			Out:    s.Out,
			ErrOut: s.ErrOut,
		},
	)

	restConfig.GroupVersion = &schema.GroupVersion{Group: "", Version: "v1"}
	restConfig.APIPath = "/api"
	restConfig.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}

	portForwardOptions.RESTClient, err = rest.RESTClientFor(restConfig)
	if err != nil {
		return fmt.Errorf("error configuring REST client: %w", err)
	}

	portForwardOptions.PodClient = clientset.CoreV1()
	portForwardOptions.Namespace = k8sCtx.Namespace
	portForwardOptions.PodName = podName
	portForwardOptions.Address = []string{s.localHost}
	portForwardOptions.Ports = []string{fmt.Sprintf("%s:%s", s.localPort, s.RemotePort)}
	portForwardOptions.Config = restConfig

	portForwardOptions.StopChannel = make(chan struct{}, 1)

	if s.ReadyChannel != nil {
		portForwardOptions.ReadyChannel = s.ReadyChannel
	} else {
		portForwardOptions.ReadyChannel = make(chan struct{})
	}

	if err = portForwardOptions.Validate(); err != nil {
		return fmt.Errorf("error validating the port-forwarding options: %w", err)
	}

	if _, err = fmt.Fprintf(s.Out, "Starting port-forward from %s to %s:%s on %s\n", s.LocalAddress, podName, s.RemotePort, s.ContextName); err != nil {
		return fmt.Errorf("error writing to output stream: %w", err)
	}

	if err = portForwardOptions.RunPortForwardContext(ctx); err != nil {
		return fmt.Errorf("error port-forwarding from %s to %s:%s on %s: %w", s.LocalAddress, podName, s.RemotePort, s.ContextName, err)
	}

	return nil
}
