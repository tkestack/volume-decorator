# volume-decorator

Controller that maintains more runtime information for Kubernetes volume(PVC), such as application identities, real usage, etc.

## Features

- Check volume availability when a workload with volumes created.
- Collect workloads attached by of a volume.
- Maintain realtime status of volumes, such as `Pending`, `Expanding`, etc.
- Collect current mounted nodes of a volume.
- Collect real usage bytes of a volume.

## Prerequisites
These build instructions assume you have a Linux build environment with:

-  Docker
-  git
-  make
-  golang  version > 1.11, with GO111MODULE=on
-  revive  `go get -u github.com/mgechev/revive`

## Build

To make the binary, just run:

```bash
make volume-decorator
```

The binary will be located at `output/bin/volume-decorator`.

## Usage

`volume-decorator` can be deployed inside the kubernetes cluster:

1. Create the RBAC objects needed by `volume-decorator`:
    ```bash
    kubectl -f deploy/kubernetes/rbac.yaml
    ```

2. Create a deployment to run the `volume-decorator`:
    ```bash
    kubectl -f deploy/kubernetes/deployment.yaml
    ```

## Examples

There are a large number of examples in [examples](examples/).
