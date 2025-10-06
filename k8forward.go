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
// If more than one pod exists in `namespace` that is labelled with app = `appName`, the first pod encountered is used.
// If `readyChan` is given, the commencement of port-forwarding can be detected by receiving from it.
// If `cancelFn` is given, it will be called upon any error except context.Canceled.
func Init(ctx context.Context, namespace, appName, localPort, remotePort string, readyChan chan struct{}, cancelFn context.CancelFunc) error {
	if err := run(ctx, namespace, appName, localPort, remotePort, readyChan); err != nil {
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

func run(ctx context.Context, namespace, appName, localPort, remotePort string, readyChan chan struct{}) error {
	if err := ValidateFlags(namespace, appName, localPort, remotePort); err != nil {
		return err
	}
	homeDir, ok := os.LookupEnv("HOME")
	if !ok {
		return fmt.Errorf("cannot locate home directory")
	}

	config, err := clientcmd.BuildConfigFromFlags("", filepath.Join(homeDir, ".kube", "config"))
	if err != nil {
		return fmt.Errorf("error loading kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("error creating k8s clientset: %w", err)
	}

	podClient := clientset.CoreV1()
	pods, err := podClient.Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", appName),
	})
	if err != nil {
		return fmt.Errorf("error listing k8s pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no k8s pods found for app '%s' in namespace %s", appName, namespace)
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
	portForwardOptions.Config = config

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
