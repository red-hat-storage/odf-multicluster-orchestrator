package odf

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	obv1alpha1 "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

func createS3Secret(ctx context.Context, rc client.Client, scheme *runtime.Scheme, name string, data map[string][]byte, namespace string, mirrorPeer *multiclusterv1alpha1.MirrorPeer) error {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, rc, &secret, func() error {
		secret.Labels = map[string]string{
			utils.CreatedByLabelKey: utils.MirrorPeerSecret,
		}
		secret.Type = corev1.SecretTypeOpaque
		secret.Data = map[string][]byte{
			utils.AwsAccessKeyId:     data[utils.AwsAccessKeyId],
			utils.AwsSecretAccessKey: data[utils.AwsSecretAccessKey],
		}
		return controllerutil.SetControllerReference(mirrorPeer, &secret, scheme)
	})

	return err
}

// ValidateAndCreateS3Secret validates the internal secret and creates the S3 secret.
// It returns the unmarshalled secret data for downstream consumers (e.g. ramen config update).
func ValidateAndCreateS3Secret(ctx context.Context, rc client.Client, scheme *runtime.Scheme, currentNamespace string, secret *corev1.Secret, mirrorPeer *multiclusterv1alpha1.MirrorPeer, logger *slog.Logger) (map[string][]byte, error) {
	logger.Info("Validating internal secret", "SecretName", secret.Name, "Namespace", secret.Namespace)

	if err := utils.ValidateInternalSecret(secret, utils.InternalLabel); err != nil {
		logger.Error("Provided internal secret is not valid", "error", err, "SecretName", secret.Name, "Namespace", secret.Namespace)
		return nil, err
	}

	data := make(map[string][]byte)
	if err := json.Unmarshal(secret.Data[utils.SecretDataKey], &data); err != nil {
		logger.Error("Failed to unmarshal secret data", "error", err, "SecretName", secret.Name, "Namespace", secret.Namespace)
		return nil, err
	}

	secretOrigin := string(secret.Data[utils.SecretOriginKey])
	logger.Info("Processing secret based on origin", "Origin", secretOrigin, "SecretName", secret.Name)

	if secretOrigin == utils.OriginMap["S3Origin"] {
		if ok := utils.ValidateS3Secret(data); !ok {
			err := fmt.Errorf("invalid S3 secret format for secret name %q in namespace %q", secret.Name, secret.Namespace)
			logger.Error("Invalid S3 secret format", "error", err, "SecretName", secret.Name, "Namespace", secret.Namespace)
			return nil, err
		}

		if err := createS3Secret(ctx, rc, scheme, secret.Name, data, currentNamespace, mirrorPeer); err != nil {
			logger.Error("Failed to create or update S3 secret", "error", err, "SecretName", secret.Name, "Namespace", currentNamespace)
			return nil, err
		}
	}

	return data, nil
}
