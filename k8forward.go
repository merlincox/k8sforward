package k8sforward

import (
	"context"
	"errors"
	"fmt"
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

// Init initiates port-forwarding with the given context `ctx` from the k8s pod with app = `appName` in the k8s
// namespace `namespace` on `remotePort` to localhost:`localPort`.
// If `versionName` is not empty, pods with `appName` will be further filtered by version = versionName.
// If more than one pod exists in `namespace` that is labelled with app = `appName` (and version = versionName, if
// applicable), the first pod encountered is used.
// If `readyChan` is given, the commencement of port-forwarding can be detected by receiving from it.
// If `cancelFn` is given, it will be called upon any error except context.Canceled.
func Init(ctx context.Context, namespace, appName, localPort, remotePort, versionName string, readyChan chan struct{}, cancelFn context.CancelFunc) error {
	if err := run(ctx, namespace, appName, localPort, remotePort, versionName, readyChan); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if cancelFn != nil {
			cancelFn()
		}
		return err
	}
	return nil
}

func run(ctx context.Context, namespace, appName, localPort, remotePort, version string, readyChan chan struct{}) error {
	if err := ValidateFlags(namespace, appName, localPort, remotePort); err != nil {
		return err
	}
	homeDir, ok := os.LookupEnv("HOME")
	if !ok {
		return fmt.Errorf("cannot locate home directory")
	}
	k8sConfigPath := filepath.Join(homeDir, ".kube", "config")

	apiConfig, err := clientcmd.LoadFromFile(k8sConfigPath)

	if err != nil {
		return fmt.Errorf("error loading k8s config file %s: %w", k8sConfigPath, err)
	}
	k8sContext := apiConfig.CurrentContext

	// Create a ClientConfig from the apiConfig
	clientConfig := clientcmd.NewDefaultClientConfig(*apiConfig, &clientcmd.ConfigOverrides{})

	// Get the *rest.Config from the ClientConfig
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("error creating k8s client REST config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("error creating k8s clientset: %w", err)
	}

	podClient := clientset.CoreV1()
	labelSelector := fmt.Sprintf("app=%s", appName)
	if version != "" {
		labelSelector = fmt.Sprintf("app=%s,version=%s", appName, version)
	}
	pods, err := podClient.Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("error listing k8s pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no k8s pods found for app '%s' in namespace %s context %s", appName, namespace, k8sContext)
	}

	var podName string
	for _, pod := range pods.Items {
		// Just pick the first pod matching the app label
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
			Out:    os.Stdout,
			ErrOut: os.Stderr,
		},
	)
	portForwardOptions.RESTClient, err = factory.RESTClient()
	if err != nil {
		return fmt.Errorf("error configuring k8s REST client: %w", err)
	}

	portForwardOptions.PodClient = clientset.CoreV1()
	portForwardOptions.Namespace = namespace
	portForwardOptions.PodName = podName
	portForwardOptions.Address = []string{"localhost"}
	portForwardOptions.Ports = []string{fmt.Sprintf("%s:%s", localPort, remotePort)}
	portForwardOptions.Config = restConfig

	portForwardOptions.StopChannel = make(chan struct{}, 1)

	if readyChan != nil {
		portForwardOptions.ReadyChannel = readyChan
	} else {
		portForwardOptions.ReadyChannel = make(chan struct{})
	}

	if err = portForwardOptions.Validate(); err != nil {
		return fmt.Errorf("error validating the k8s portforward options: %w", err)
	}

	fmt.Printf("Starting k8s port forward from localhost:%s to %s:%s\n", localPort, podName, remotePort)
	if err = portForwardOptions.RunPortForwardContext(ctx); err != nil {
		return fmt.Errorf("error port forwarding to k8s pod: %w", err)
	}

	return nil
}
