package controllers

import (
	"context"
	"errors"
	"log/slog"
	"reflect"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// NamedPeerRefWithSecretData is an intermediate data structure,
// when converting a source secret to corresponding peer destination secret
type NamedPeerRefWithSecretData struct {
	multiclusterv1alpha1.PeerRef
	Name         string
	Data         []byte
	SecretOrigin string
	StorageID    string
}

// NewNamedPeerRefWithSecretData creates a new PeerRef instance which has the source Name and Data details
// attached to it.
// Make sure we give a source secret and it's (one of the) connected PeerRef as arguments.
func NewNamedPeerRefWithSecretData(secret *corev1.Secret, peerRef multiclusterv1alpha1.PeerRef) *NamedPeerRefWithSecretData {
	var nPeerRef NamedPeerRefWithSecretData = NamedPeerRefWithSecretData{
		Name:         secret.Name,
		Data:         secret.Data[utils.SecretDataKey],
		PeerRef:      peerRef,
		SecretOrigin: string(secret.Data[utils.SecretOriginKey]),
		StorageID:    string(secret.Data[utils.SecretStorageIDKey]),
	}
	return &nPeerRef
}

// GenerateSecret will generate a secret of provided type.
// nPeerRef object contains TWO parts,
//
//	a. details of the secret, like name,namespace etc; that is to be generated
//	b. data part that is to be passed on to the newly created secret
//
// The type of secret (ie; source or destination) is passed as the argument
func (nPR *NamedPeerRefWithSecretData) GenerateSecret(secretLabelType utils.SecretLabelType) *corev1.Secret {
	if nPR == nil {
		return nil
	}
	secretNamespacedName := types.NamespacedName{
		Name:      nPR.Name,
		Namespace: nPR.PeerRef.ClusterName,
	}
	storageClusterNamespacedName := types.NamespacedName{
		Name:      nPR.PeerRef.StorageClusterRef.Name,
		Namespace: nPR.PeerRef.StorageClusterRef.Namespace,
	}
	var retSecret *corev1.Secret
	if secretLabelType == utils.DestinationLabel {
		retSecret = utils.CreateDestinationSecret(secretNamespacedName, storageClusterNamespacedName, nPR.Data, nPR.SecretOrigin, nPR.StorageID)
	} else if secretLabelType == utils.SourceLabel {
		retSecret = utils.CreateSourceSecret(secretNamespacedName, storageClusterNamespacedName, nPR.Data, nPR.SecretOrigin, nPR.StorageID)
	}

	return retSecret
}

func (nPR *NamedPeerRefWithSecretData) Request() (req reconcile.Request) {
	if nPR == nil {
		return
	}
	req.NamespacedName = types.NamespacedName{
		Name:      nPR.Name,
		Namespace: nPR.PeerRef.ClusterName,
	}
	return
}

func (nPR *NamedPeerRefWithSecretData) ErrorOnNilReceiver() (err error) {
	if nPR == nil {
		err = errors.New("'nil' receiver detected")
	}
	return err
}

// GetAssociatedSecret returns a secret in the cluster, which has the
// same 'Name' and 'Namespace' matching the PeerRef's name and clustername
func (nPR *NamedPeerRefWithSecretData) GetAssociatedSecret(ctx context.Context, rc client.Client, holdSecret *corev1.Secret) error {
	if err := nPR.ErrorOnNilReceiver(); err != nil {
		return err
	}

	var err error
	localSecret := corev1.Secret{}
	req := nPR.Request()
	err = rc.Get(ctx, req.NamespacedName, &localSecret)
	if err == nil && holdSecret != nil {
		localSecret.DeepCopyInto(holdSecret)
	}
	return err
}

// CreateOrUpdateDestinationSecret creates/updates the destination secret from NamedPeerRefWithSecretData object
func (nPR *NamedPeerRefWithSecretData) CreateOrUpdateDestinationSecret(ctx context.Context, rc client.Client, logger *slog.Logger) error {
	err := nPR.ErrorOnNilReceiver()
	if err != nil {
		logger.Error("Receiver is nil", "error", err)
		return err
	}

	expectedDest := nPR.GenerateSecret(utils.DestinationLabel)
	var currentDest corev1.Secret
	err = nPR.GetAssociatedSecret(ctx, rc, &currentDest)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Creating destination secret", "SecretName", expectedDest.Name, "Namespace", expectedDest.Namespace)
			return rc.Create(ctx, expectedDest)
		}
		logger.Error("Unable to get the destination secret", "DestinationRef", nPR.PeerRef, "error", err)
		return err
	}

	if !reflect.DeepEqual(expectedDest.Data, currentDest.Data) {
		logger.Info("Updating the destination secret", "SecretName", currentDest.Name, "Namespace", currentDest.Namespace)
		_, err := controllerutil.CreateOrUpdate(ctx, rc, &currentDest, func() error {
			currentDest.Data = expectedDest.Data
			return nil
		})
		if err != nil {
			logger.Error("Failed to update destination secret", "SecretName", currentDest.Name, "Namespace", currentDest.Namespace, "error", err)
		}
		return err
	}

	logger.Info("Destination secret is up-to-date", "SecretName", currentDest.Name, "Namespace", currentDest.Namespace)
	return nil
}
