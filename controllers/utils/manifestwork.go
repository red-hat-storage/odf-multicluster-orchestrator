package utils

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func CreateOrUpdateManifestWork(ctx context.Context, c client.Client, name string, namespace string, objJson []byte, manifestConfigOptions []workv1.ManifestConfigOption, ownerRef metav1.OwnerReference) (controllerutil.OperationResult, error) {
	mw := workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				ownerRef,
			},
		},
	}

	operationResult, err := controllerutil.CreateOrUpdate(ctx, c, &mw, func() error {
		mw.Spec = workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{
					{
						RawExtension: runtime.RawExtension{
							Raw: objJson,
						},
					},
				},
			},
		}
		if len(manifestConfigOptions) > 0 {
			mw.Spec.ManifestConfigs = manifestConfigOptions
		}
		return nil
	})

	if err != nil {
		return operationResult, fmt.Errorf("failed to create and update ManifestWork %s for namespace %s. error %w", name, namespace, err)
	}

	return operationResult, nil
}

func GetManifestWork(ctx context.Context, c client.Client, manifestWorkName string, namespace string) (*workv1.ManifestWork, error) {
	var manifestWork workv1.ManifestWork

	if err := c.Get(ctx, types.NamespacedName{
		Name:      manifestWorkName,
		Namespace: namespace,
	}, &manifestWork); err != nil {
		return nil, fmt.Errorf("failed to get ManifestWork %s in namespace %s: %w", manifestWorkName, namespace, err)
	}

	return &manifestWork, nil
}
