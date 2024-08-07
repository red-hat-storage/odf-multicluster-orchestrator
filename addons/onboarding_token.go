package addons

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/red-hat-storage/odf-multicluster-orchestrator/addons/setup"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/red-hat-storage/odf-multicluster-orchestrator/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func requestStorageClusterPeerToken(ctx context.Context, proxyServiceNamespace string) (string, error) {
	token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return "", fmt.Errorf("failed to read token: %w", err)
	}
	url := fmt.Sprintf("https://ux-backend-proxy.%s.svc.cluster.local:8888/onboarding-tokens", proxyServiceNamespace)
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", string(token)))

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read http response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %s", http.StatusText(resp.StatusCode))
	}

	return string(body), nil
}

func createStorageClusterPeerTokenSecret(ctx context.Context, client client.Client, scheme *runtime.Scheme, spokeClusterName string, odfOperatorNamespace string, mirrorPeer multiclusterv1alpha1.MirrorPeer, storageClusterRef *v1alpha1.StorageClusterRef) error {
	uniqueSecretName := string(mirrorPeer.GetUID())
	_, err := utils.FetchSecretWithName(ctx, client, types.NamespacedName{Namespace: spokeClusterName, Name: uniqueSecretName})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get secret %s/%s: %w", spokeClusterName, uniqueSecretName, err)
	}
	if err == nil {
		return errors.NewAlreadyExists(corev1.Resource("secret"), uniqueSecretName)
	}

	token, err := requestStorageClusterPeerToken(ctx, odfOperatorNamespace)
	if err != nil {
		return fmt.Errorf("unable to generate StorageClusterPeer token. %w", err)
	}

	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      uniqueSecretName,
			Namespace: spokeClusterName,
			Labels: map[string]string{
				utils.CreatedByLabelKey:  setup.TokenExchangeName,
				utils.SecretLabelTypeKey: string(utils.InternalLabel),
				utils.HubRecoveryLabel:   "",
			},
		},
		Data: map[string][]byte{
			utils.NamespaceKey:          []byte(storageClusterRef.Namespace),
			utils.StorageClusterNameKey: []byte(storageClusterRef.Name),
			utils.SecretDataKey:         []byte(token),
		},
	}

	err = controllerutil.SetOwnerReference(&mirrorPeer, tokenSecret, scheme)
	if err != nil {
		return fmt.Errorf("failed to set owner reference for secret %s/%s: %w", spokeClusterName, uniqueSecretName, err)
	}

	return client.Create(ctx, tokenSecret)
}

func deleteStorageClusterPeerTokenSecret(ctx context.Context, client client.Client, tokenNamespace string, tokenName string) error {
	token := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tokenName,
			Namespace: tokenNamespace,
		},
	}

	err := client.Delete(ctx, token)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}
