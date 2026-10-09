# ODF Multicluster Orchestrator API

This is a Go submodule containing the API types for ODF Multicluster Orchestrator. This module can be imported independently without requiring the entire codebase.

## Usage

To use this API module in your project, import it like this:

```go
import (
    multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
)
```

Add the dependency to your `go.mod`:

```bash
go get github.com/red-hat-storage/odf-multicluster-orchestrator/api@latest
```

## Available API Types

This module includes the following Custom Resource Definitions (CRDs):

- **MirrorPeer**: Defines peer relationships between managed clusters for data replication
- **ProtectedApplicationView**: Provides a view of protected applications across clusters  
- **S3Configuration**: Configures S3-compatible storage for multicluster orchestration

## API Version

Current API version: `v1alpha1`

Group: `multicluster.odf.openshift.io`

## Example

```go
package main

import (
    multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
    "k8s.io/apimachinery/pkg/runtime"
    clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func main() {
    scheme := runtime.NewScheme()
    _ = clientgoscheme.AddToScheme(scheme)
    _ = multiclusterv1alpha1.AddToScheme(scheme)
    
    // Use the API types in your code
}
```

## License

Licensed under the Apache License, Version 2.0. See the LICENSE file in the repository root for details.
