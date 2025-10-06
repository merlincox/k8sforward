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
	namespace := flag.String("n", "", "k8s namespace")
	appName := flag.String("app", "", "k8s app name")
	version := flag.String("version", "", "version (optional)")
	localPort := flag.String("local-port", "", "localhost TCP port to use")
	remotePort := flag.String("remote-port", "", "remote TCP port to use")
	flag.Parse()
	if err := k8sforward.ValidateFlags(*namespace, *appName, *localPort, *remotePort); err != nil {
		return err
	}

	return k8sforward.Init(context.Background(), *namespace, *appName, *localPort, *remotePort, *version, nil, nil)
}
