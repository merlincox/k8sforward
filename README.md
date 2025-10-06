### k8sforward

Utility for k8s port-forwarding.

Use case: standalone code needing to access k8s pod GRPC API, etc. withoout having to set up port-forwarding separately.

```
import "github.com/merlincox/k8sforward"

....

var pfErr error
ctx, cancelFn := context.WithCancel(priorCtx)
defer cancelFunc()
readyChan := make(chan struct{})

settings := &k8sforward.Settings{
  ContextName:    contextName,
  AppName:        appName,
  LocalPort:      localPort,
  RemotePort:     remotePort,
  VersionName:    versionName,
  KubeconfigPath: kubeconfigPath,
  ReadyChannel:   readyChan,
  CancelFn:       cancelFunc,
}

go func() {
  pfErr = k8sforward.Init(ctx, settings)
}()
select {
  case <- readyChan:
  case <- ctx.Done()
}

if pfErr != nil {
  return pfErr
}

// do something requiring port-forwarding using ctx as context.
```

Alternatively, compile `cmd/main.go` and use the compiled executable from the command line.