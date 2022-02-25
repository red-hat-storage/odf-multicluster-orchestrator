package controllers

import (
	"context"
	"errors"
	"reflect"

	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/common"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// NamedPeerRefWithSecretData is an intermediate data structure,
// when converting a source secret to corresponding peer destination secret
type NamedPeerRefWithSecretData struct {
	multiclusterv1alpha1.PeerRef
	Name         string
	Data         []byte
	SecretOrigin string
}

// NewNamedPeerRefWithSecretData creates a new PeerRef instance which has the source Name and Data details
// attached to it.
// Make sure we give a source secret and it's (one of the) connected PeerRef as arguments.
func NewNamedPeerRefWithSecretData(secret *corev1.Secret, peerRef multiclusterv1alpha1.PeerRef) *NamedPeerRefWithSecretData {
	var nPeerRef NamedPeerRefWithSecretData = NamedPeerRefWithSecretData{
		Name:         secret.Name,
		Data:         secret.Data[common.SecretDataKey],
		PeerRef:      peerRef,
		SecretOrigin: string(secret.Data[common.SecretOriginKey]),
	}
	return &nPeerRef
}

// GenerateSecret will generate a secret of provided type.
// nPeerRef object contains TWO parts,
//	 a. details of the secret, like name,namespace etc; that is to be generated
// 	 b. data part that is to be passed on to the newly created secret
// The type of secret (ie; source or destination) is passed as the argument
func (nPR *NamedPeerRefWithSecretData) GenerateSecret(secretLabelType common.SecretLabelType) *corev1.Secret {
	if nPR == nil {
		return nil
	}
	secretNamespacedName := types.NamespacedName{
		Name:      nPR.Name,
		Namespace: nPR.ClusterName,
	}
	storageClusterNamespacedName := types.NamespacedName{
		Name:      nPR.StorageClusterRef.Name,
		Namespace: nPR.StorageClusterRef.Namespace,
	}
	var retSecret *corev1.Secret
	if secretLabelType == common.DestinationLabel {
		retSecret = common.CreateDestinationSecret(secretNamespacedName, storageClusterNamespacedName, nPR.Data, nPR.SecretOrigin)
	} else if secretLabelType == common.SourceLabel {
		retSecret = common.CreateSourceSecret(secretNamespacedName, storageClusterNamespacedName, nPR.Data, nPR.SecretOrigin)
	}

	return retSecret
}

func (nPR *NamedPeerRefWithSecretData) Request() (req reconcile.Request) {
	if nPR == nil {
		return
	}
	req.NamespacedName = types.NamespacedName{
		Name:      nPR.Name,
		Namespace: nPR.ClusterName,
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
func (nPR *NamedPeerRefWithSecretData) CreateOrUpdateDestinationSecret(ctx context.Context, rc client.Client) error {
	err := nPR.ErrorOnNilReceiver()
	if err != nil {
		return err
	}

	logger := log.FromContext(ctx)
	expectedDest := nPR.GenerateSecret(common.DestinationLabel)
	var currentDest corev1.Secret
	err = nPR.GetAssociatedSecret(ctx, rc, &currentDest)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("Creating destination secret", "secret", expectedDest.Name, "namespace", expectedDest.Namespace)
			return rc.Create(ctx, expectedDest)
		}
		logger.Error(err, "Unable to get the destination secret", "destination-ref", nPR.PeerRef)
		return err
	}

	// recieved a destination secret, now compare
	if !reflect.DeepEqual(expectedDest.Data, currentDest.Data) {
		logger.Info("Updating the destination secret", "secret", currentDest.Name, "namespace", currentDest.Namespace)
		_, err := controllerutil.CreateOrUpdate(ctx, rc, &currentDest, func() error {
			currentDest.Data = expectedDest.Data
			return nil
		})
		return err
	}
	return nil
}
