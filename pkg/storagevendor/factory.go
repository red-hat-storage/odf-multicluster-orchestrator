/*
Copyright 2026 Red Hat Data Foundation.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package storagevendor

import (
	"context"
	"fmt"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/pkg/storagevendor/odf"

	"k8s.io/apimachinery/pkg/runtime"
	"log/slog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// New returns the appropriate storage vendor implementation
func New(ctx context.Context, cl client.Client, scheme *runtime.Scheme, logger *slog.Logger, currentNamespace string, vendorType multiclusterv1alpha1.VendorType) (StorageVendor, error) {
	switch vendorType {
	case multiclusterv1alpha1.ODF:
		return odf.NewODFVendor(ctx, cl, scheme, logger, currentNamespace)
	default:
		return nil, fmt.Errorf("unsupported storage vendor: %s", vendorType)
	}
}
