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
go func() {
  pfErr = k8sforward.Init(ctx, namespace, appName, localPort, remotePort, readyChan, cancelFn)
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

Alternatively compile and use from the commandline.