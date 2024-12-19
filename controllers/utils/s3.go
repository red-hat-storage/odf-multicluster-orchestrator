package utils

import (
	"context"
	"fmt"
	"os"

	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	BucketGenerateName = "odrbucket"

	// s3
	S3ProfilePrefix    = "s3profile"
	S3Endpoint         = "s3CompatibleEndpoint"
	S3BucketName       = "s3Bucket"
	S3ProfileName      = "s3ProfileName"
	S3Region           = "s3Region"
	AwsAccessKeyId     = "AWS_ACCESS_KEY_ID"
	AwsSecretAccessKey = "AWS_SECRET_ACCESS_KEY"

	// ramen
	RamenHubOperatorConfigName = "ramen-hub-operator-config"

	//handlers
	RookSecretHandlerName       = "rook"
	S3SecretHandlerName         = "s3"
	DRModeAnnotationKey         = "multicluster.openshift.io/mode"
	MirrorPeerNameAnnotationKey = "multicluster.odf.openshift.io/mirrorpeer"
)

func GetCurrentStorageClusterRef(mp *multiclusterv1alpha1.MirrorPeer, spokeClusterName string) (*multiclusterv1alpha1.StorageClusterRef, error) {
	for _, v := range mp.Spec.Items {
		if v.ClusterName == spokeClusterName {
			return &v.StorageClusterRef, nil
		}
	}
	return nil, fmt.Errorf("StorageClusterRef for cluster %s under mirrorpeer %s not found", spokeClusterName, mp.Name)
}

func GetEnv(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}

func GenerateBucketName(mirrorPeer multiclusterv1alpha1.MirrorPeer, hasStorageClientRef bool) string {
	mirrorPeerId := GenerateUniqueIdForMirrorPeer(mirrorPeer, hasStorageClientRef)
	bucketGenerateName := BucketGenerateName
	return fmt.Sprintf("%s-%s", bucketGenerateName, mirrorPeerId)[0 : len(BucketGenerateName)+1+12]
}

func CreateOrUpdateObjectBucketClaim(ctx context.Context, c client.Client, bucketName, bucketNamespace string, annotations map[string]string) (controllerutil.OperationResult, error) {
	noobaaOBC := &obv1alpha1.ObjectBucketClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        bucketName,
			Namespace:   bucketNamespace,
			Annotations: annotations, // Set annotations here
		},
	}

	operationResult, err := controllerutil.CreateOrUpdate(ctx, c, noobaaOBC, func() error {
		noobaaOBC.Spec = obv1alpha1.ObjectBucketClaimSpec{
			BucketName:       bucketName,
			StorageClassName: fmt.Sprintf("%s.noobaa.io", bucketNamespace),
		}

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
