# podinfo-operator
Sample Operator for https://github.com/stefanprodan/podinfo


# Running tests 

## Unit tests 

Unit tests can be ran using 
``` 
make test
```

## E2E Tests

End to end tests can be executed using 
``` 
kind create cluster # Kind cluster is required, skip if you have one already
make test-e2e
```
