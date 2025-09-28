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

func Run(ctx context.Context, namespace, appName, localPort, remotePort string) error {
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
		return fmt.Errorf("error creating clientset: %w", err)
	}

	podClient := clientset.CoreV1()
	pods, err := podClient.Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", appName),
	})
	if err != nil {
		return fmt.Errorf("error listing k8s pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no k8s pods found for app %s in namespace %s", appName, namespace)
	}

	var podName string
	for _, pod := range pods.Items {
		// Just pick the first pod matching the app label
		podName = pod.Name
		break
	}

	kubeConfigFlags := genericclioptions.NewConfigFlags(true)
	matchVersionKubeConfigFlags := util.NewMatchVersionFlags(kubeConfigFlags)
	factory := util.NewFactory(matchVersionKubeConfigFlags)

	portForwardOptions := portforward.NewDefaultPortForwardOptions(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	})

	portForwardOptions.PodClient = clientset.CoreV1()
	portForwardOptions.Namespace = namespace
	portForwardOptions.PodName = podName
	portForwardOptions.Address = []string{"localhost"}
	portForwardOptions.Ports = []string{fmt.Sprintf("%s:%s", localPort, remotePort)}
	portForwardOptions.Config = config
	portForwardOptions.RESTClient, err = factory.RESTClient()
	if err != nil {
		return fmt.Errorf("error configuring REST client: %w", err)
	}

	portForwardOptions.StopChannel = make(chan struct{}, 1)
	portForwardOptions.ReadyChannel = make(chan struct{})

	if err = portForwardOptions.Validate(); err != nil {
		return fmt.Errorf("error validating the portforward options: %w", err)
	}

	fmt.Printf("Starting port forward from localhost:%s to %s:%s\n", localPort, podName, remotePort)
	if err = portForwardOptions.RunPortForwardContext(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Printf("forwarding cancelled\n")
			return nil
		}
		return fmt.Errorf("error forwarding: %w", err)
	}

	return nil
}
