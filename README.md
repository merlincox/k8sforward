### k8sforward

Utility for k8s port forwarding.

Use case: standalone code needing to access k8s pod GRPC API, etc. withoout having to set up port-forwarding separately.

```
var pfErr error
ctx, cancelFnc := context.WithCancel(priorCtx)
defer cancelFunc()
readyChan := make(chan struct{})
go func() {
  pfErr = k8sforward(ctx, namespace, appName, localPort, remotePort, readyChan, cancelFunc)
}()
select {
  case <- readyChan:
  case <- ctx.Done()
}

if pfErr != nil {
  return pfErr
}

// do something requiring port-forwarding using ctx.
```

