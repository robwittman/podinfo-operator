# podinfo-operator
Sample Operator for https://github.com/stefanprodan/podinfo

## Getting Started 

Create a local kubernetes cluster with your preferred method. Then we can run the operator either 
in cluster, or locally

### In-Cluster 
``` 
make install deploy  
```

### Locally 
``` 
make install run 
```

Once the operator is running, we can apply the sample CRD using 
```
kubectl apply -f config/samples/apps_v1alpha1_podinfo.yaml
```

This will install [Bitnami's Redis Chart](https://artifacthub.io/packages/helm/bitnami/redis), as 
well as the [PodInfo Service](https://github.com/stefanprodan/podinfo) connected to it. 

### Connecting to PodInfo 

The operator does no[podinfo_controller_test.go](internal%2Fcontroller%2Fpodinfo_controller_test.go)t deploy an ingress resource for the podinfo service. To connect to it, 
port-forward to the service 
```shell 
kubectl port-forward svc/podinfo-sample -n default 9898
```

## Running tests 

### Unit tests 

Unit tests can be ran using 
``` 
make test
```

### E2E Tests
End to end tests can be executed using 
``` 
kind create cluster # Kind cluster is required, skip if you have one already
make test-e2e
```
