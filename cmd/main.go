package main

import (
	"context"
	"flag"
	"fmt"
	"io"
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
	localAddress := flag.String("local-address", "", "local address to use (such as 'localhost:8080')")
	remotePort := flag.String("remote-port", "", "remote TCP port to use")

	versionName := flag.String("app-version", "", "app version (optional)")
	kubeconfigPath := flag.String("kubeconfig-path", "", "kubeconfig Path (optional)")
	silent := flag.Bool("silent", false, "silence non-error output (optional)")

	flag.Parse()
	settings := &k8sforward.Settings{
		ContextName:    *contextName,
		AppName:        *appName,
		LocalAddress:   *localAddress,
		RemotePort:     *remotePort,
		VersionName:    *versionName,
		KubeconfigPath: *kubeconfigPath,
	}
	if silent != nil && *silent {
		settings.Out = io.Discard
	}

	if err := settings.Validate(); err != nil {
		return err
	}

	return k8sforward.Init(context.Background(), settings)
}
