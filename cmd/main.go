package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/merlincox/k8sforward"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run() error {
	contextName := flag.String("n", "", "k8s context name")
	appName := flag.String("app", "", "k8s app name")
	localPort := flag.String("local-port", "", "localhost TCP port to use")
	remotePort := flag.String("remote-port", "", "remote TCP port to use")

	versionName := flag.String("app-version", "", "app version (optional)")
	kubeconfigPath := flag.String("kubeconfig-path", "", "kubeconfig Path (optional)")

	flag.Parse()
	settings := &k8sforward.Settings{
		ContextName:    *contextName,
		AppName:        *appName,
		LocalPort:      *localPort,
		RemotePort:     *remotePort,
		VersionName:    *versionName,
		KubeconfigPath: *kubeconfigPath,
	}

	if err := settings.Validate(); err != nil {
		return err
	}

	return k8sforward.Init(context.Background(), settings)
}
