package k8sforward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"k8s.io/client-go/kubernetes/typed/core/v1"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/cmd/portforward"
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

	localHost          string
	localPort          string
	namespace          string
	restConfig         *rest.Config
	podClient          v1.CoreV1Interface
	portForwardOptions *portforward.PortForwardOptions

	validated bool
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

	return s.prepare()
}

func (s *Settings) prepare() error {
	apiConfig, err := clientcmd.LoadFromFile(s.KubeconfigPath)
	if err != nil {
		return fmt.Errorf("error loading the k8s config from %s: %w", s.KubeconfigPath, err)
	}

	k8sCtx, ok := apiConfig.Contexts[s.ContextName]
	if !ok {
		return fmt.Errorf("unknown k8s context '%s'", s.ContextName)
	}

	s.namespace = k8sCtx.Namespace

	clientConfig := clientcmd.NewDefaultClientConfig(*apiConfig, &clientcmd.ConfigOverrides{
		CurrentContext: s.ContextName,
	})

	s.restConfig, err = clientConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("error creating the k8s client REST config: %w", err)
	}
	s.restConfig.GroupVersion = &schema.GroupVersion{Group: "", Version: "v1"}
	s.restConfig.APIPath = "/api"
	s.restConfig.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}

	clientset, err := kubernetes.NewForConfig(s.restConfig)
	if err != nil {
		return fmt.Errorf("error creating k8s client set: %w", err)
	}

	s.podClient = clientset.CoreV1()
	s.portForwardOptions = portforward.NewDefaultPortForwardOptions(
		genericiooptions.IOStreams{
			In:     os.Stdin,
			Out:    s.Out,
			ErrOut: s.ErrOut,
		},
	)
	s.portForwardOptions.RESTClient, err = rest.RESTClientFor(s.restConfig)
	if err != nil {
		return fmt.Errorf("error configuring REST client: %w", err)
	}

	s.portForwardOptions.PodClient = s.podClient
	s.portForwardOptions.Namespace = s.namespace
	s.portForwardOptions.PodName = "placeholder"
	s.portForwardOptions.Address = []string{s.localHost}
	s.portForwardOptions.Ports = []string{fmt.Sprintf("%s:%s", s.localPort, s.RemotePort)}
	s.portForwardOptions.Config = s.restConfig

	s.portForwardOptions.StopChannel = make(chan struct{}, 1)

	if s.ReadyChannel != nil {
		s.portForwardOptions.ReadyChannel = s.ReadyChannel
	} else {
		s.portForwardOptions.ReadyChannel = make(chan struct{})
	}
	if err = s.portForwardOptions.Validate(); err != nil {
		return fmt.Errorf("error validating the port-forwarding options: %w", err)
	}

	s.validated = true

	return nil
}

func (s *Settings) run(ctx context.Context) error {
	if err := s.Validate(); err != nil {
		return err
	}

	labelSelector := fmt.Sprintf("app=%s", s.AppName)
	missingErrMsg := fmt.Sprintf("no running pods found for app '%s' in '%s' context", s.AppName, s.ContextName)

	if s.VersionName != "" {
		labelSelector = fmt.Sprintf("app=%s,version=%s", s.AppName, s.VersionName)
		missingErrMsg = fmt.Sprintf("no running pods found for app '%s' version '%s' in '%s' context", s.AppName, s.VersionName, s.ContextName)
	}

	listOptions := metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: "status.phase=Running",
	}

	pods, err := s.podClient.Pods(s.namespace).List(ctx, listOptions)
	if err != nil {
		return fmt.Errorf("error listing pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return fmt.Errorf(missingErrMsg)
	}

	for _, pod := range pods.Items {
		// Just pick the first running pod matching the label selector
		s.portForwardOptions.PodName = pod.Name
		break
	}

	if _, err = fmt.Fprintf(s.Out, "Starting port-forward from %s to %s:%s on %s\n", s.LocalAddress, s.portForwardOptions.PodName, s.RemotePort, s.ContextName); err != nil {
		return fmt.Errorf("error writing to output stream: %w", err)
	}

	if err = s.portForwardOptions.RunPortForwardContext(ctx); err != nil {
		return fmt.Errorf("error port-forwarding from %s to %s:%s on %s: %w", s.LocalAddress, s.portForwardOptions.PodName, s.RemotePort, s.ContextName, err)
	}

	return nil
}
