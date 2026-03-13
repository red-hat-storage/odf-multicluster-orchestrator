package odf

import (
	"context"
	"fmt"

	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	BucketGenerateName = "odrbucket"
)

func GetCurrentStorageClusterRef(mp *multiclusterv1alpha1.MirrorPeer, spokeClusterName string) (*multiclusterv1alpha1.StorageClusterRef, error) {
	for _, v := range mp.Spec.Items {
		if v.ClusterName == spokeClusterName {
			return &v.StorageClusterRef, nil
		}
	}

	return nil, fmt.Errorf("StorageClusterRef for cluster %s under mirrorpeer %s not found", spokeClusterName, mp.Name)
}

func GenerateBucketName(mirrorPeer *multiclusterv1alpha1.MirrorPeer) string {
	mirrorPeerId := utils.GenerateUniqueIdForMirrorPeer(mirrorPeer)
	return fmt.Sprintf("%s-%s", BucketGenerateName, mirrorPeerId)[0 : len(BucketGenerateName)+1+12]
}

func CreateOrUpdateObjectBucketClaim(ctx context.Context, c client.Client, bucketName, bucketNamespace string, annotations map[string]string) (controllerutil.OperationResult, error) {
	noobaaOBC := &obv1alpha1.ObjectBucketClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        bucketName,
			Namespace:   bucketNamespace,
			Annotations: annotations,
		},
	}

	operationResult, err := controllerutil.CreateOrUpdate(ctx, c, noobaaOBC, func() error {
		noobaaOBC.Spec.BucketName = bucketName
		noobaaOBC.Spec.StorageClassName = fmt.Sprintf("%s.noobaa.io", bucketNamespace)

		if noobaaOBC.Annotations == nil {
			noobaaOBC.Annotations = annotations
		} else {
			for key, value := range annotations {
				noobaaOBC.Annotations[key] = value
			}
		}

		return nil
	})

	if err != nil {
		return controllerutil.OperationResultNone, fmt.Errorf("failed to create or update ObjectBucketClaim %s/%s: %w", bucketNamespace, bucketName, err)
	}

	return operationResult, nil
}

func GetObjectBucketClaim(ctx context.Context, c client.Client, bucketName, bucketNamespace string) (*obv1alpha1.ObjectBucketClaim, error) {
	noobaaOBC := &obv1alpha1.ObjectBucketClaim{}
	err := c.Get(ctx, client.ObjectKey{
		Name:      bucketName,
		Namespace: bucketNamespace,
	}, noobaaOBC)

	if err != nil {
		return nil, fmt.Errorf("failed to fetch ObjectBucketClaim %s/%s: %w", bucketNamespace, bucketName, err)
	}

	return noobaaOBC, nil
}
