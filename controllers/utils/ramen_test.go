//go:build unit
// +build unit

/*
Copyright 2021 Red Hat OpenShift Data Foundation.

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

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	rmn "github.com/ramendr/ramen/api/v1alpha1"
	multiclusterv1alpha1 "github.com/red-hat-storage/odf-multicluster-orchestrator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	yaml "sigs.k8s.io/yaml"
)

var (
	SecretName                         = "s3Secret"
	TestS3BucketName                   = "s3bucket"
	TestAwsAccessKeyId                 = "awskeyid"
	TestAwssecretaccesskey             = "awsaccesskey"
	TestS3RouteHost                    = "https://s3.endpoint"
	TestSourceManagedClusterEast       = "east"
	TestDestinationManagedClusterWest  = "west"
	TestSourceManagedClusterSoth       = "south"
	TestDestinationManagedClusterNorth = "north"

	ramenNamespace          = "openshift-dr-system"
	StorageClusterName      = "ocs-storagecluster"
	StorageClusterNamespace = "openshift-storage"
)

func fakeMirrorPeers(manageS3 bool) multiclusterv1alpha1.MirrorPeer {
	return multiclusterv1alpha1.MirrorPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mirrorpeer",
		},
		Spec: multiclusterv1alpha1.MirrorPeerSpec{
			ManageS3: manageS3,
			Items: []multiclusterv1alpha1.PeerRef{
				{
					ClusterName: TestSourceManagedClusterEast,
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      StorageClusterName,
						Namespace: StorageClusterNamespace,
					},
				},
				{
					ClusterName: TestDestinationManagedClusterWest,
					StorageClusterRef: multiclusterv1alpha1.StorageClusterRef{
						Name:      StorageClusterName,
						Namespace: StorageClusterNamespace,
					},
				},
			},
		},
	}
}

func fakeS3InternalSecret(t *testing.T, clusterName string) *corev1.Secret {
	secretData, err := json.Marshal(
		map[string][]byte{
			AwsAccessKeyId:     []byte(TestAwsAccessKeyId),
			AwsSecretAccessKey: []byte(TestAwssecretaccesskey),
			S3BucketName:       []byte(TestS3BucketName),
			S3Endpoint:         []byte(TestS3RouteHost),
			S3Region:           []byte(""),
			S3ProfileName:      []byte(fmt.Sprintf("%s-%s-%s", S3ProfilePrefix, clusterName, StorageClusterName)),
		},
	)
	assert.NoError(t, err)

	data := map[string][]byte{
		SecretDataKey:         secretData,
		NamespaceKey:          []byte(StorageClusterNamespace),
		StorageClusterNameKey: []byte(StorageClusterName),
		SecretOriginKey:       []byte(OriginMap["S3Origin"]),
	}
	expectedSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CreateUniqueSecretName(clusterName, StorageClusterNamespace, StorageClusterName, S3ProfilePrefix),
			Namespace: clusterName,
			Labels: map[string]string{
				SecretLabelTypeKey: string(InternalLabel),
			},
			Annotations: map[string]string{
				MirrorPeerNameAnnotationKey: "mirrorpeer",
			},
		},
		Type: SecretLabelTypeKey,
		Data: data,
	}

	return &expectedSecret
}

func getFakeClient(t *testing.T, mgrScheme *runtime.Scheme) client.Client {
	emptyConfig, err := yaml.Marshal(rmn.RamenConfig{})
	assert.NoError(t, err)
	filledConfig1, err := yaml.Marshal(rmn.RamenConfig{
		S3StoreProfiles: getS3Profile("namespace1", TestSourceManagedClusterSoth, TestDestinationManagedClusterNorth),
	})
	assert.NoError(t, err)
	filledConfig2, err := yaml.Marshal(rmn.RamenConfig{
		S3StoreProfiles: getS3Profile("namespace2", TestSourceManagedClusterEast, TestDestinationManagedClusterWest),
	})
	assert.NoError(t, err)

	obj := []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      RamenHubOperatorConfigName,
				Namespace: ramenNamespace,
			},
			Data: map[string]string{
				"ramen_manager_config.yaml": string(emptyConfig),
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      RamenHubOperatorConfigName,
				Namespace: "namespace1",
			},
			Data: map[string]string{
				"ramen_manager_config.yaml": string(filledConfig1),
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      RamenHubOperatorConfigName,
				Namespace: "namespace2",
			},
			Data: map[string]string{
				"ramen_manager_config.yaml": string(filledConfig2),
			},
		},
	}
	return fake.NewClientBuilder().WithScheme(mgrScheme).WithRuntimeObjects(obj...).Build()
}

func getRamenConfig(t *testing.T, ctx context.Context, fakeClient client.Client, ramenNamespace string) rmn.RamenConfig {
	currentRamenConfigMap := corev1.ConfigMap{}
	namespacedName := types.NamespacedName{
		Name:      RamenHubOperatorConfigName,
		Namespace: ramenNamespace,
	}
	err := fakeClient.Get(ctx, namespacedName, &currentRamenConfigMap)
	assert.NoError(t, err)
	ramenConfigData := currentRamenConfigMap.Data["ramen_manager_config.yaml"]
	ramenConfig := rmn.RamenConfig{}
	err = yaml.Unmarshal([]byte(ramenConfigData), &ramenConfig)
	assert.NoError(t, err)

	return ramenConfig
}

func getRamenS3Secret(secretName string, ctx context.Context, fakeClient client.Client, ramenNamespace string) (corev1.Secret, error) {
	ramenSecret := corev1.Secret{}
	namespacedName := types.NamespacedName{
		Name:      CreateUniqueSecretName(secretName, StorageClusterNamespace, StorageClusterName, S3ProfilePrefix),
		Namespace: ramenNamespace,
	}
	err := fakeClient.Get(ctx, namespacedName, &ramenSecret)

	return ramenSecret, err

}

func getS3Profile(ramenNamespace string, sourceManagedClusterName string, destinationManagedClusterName string) []rmn.S3StoreProfile {
	return []rmn.S3StoreProfile{
		{
			S3ProfileName:        fmt.Sprintf("%s-%s-%s", S3ProfilePrefix, sourceManagedClusterName, StorageClusterName),
			S3Bucket:             TestS3BucketName,
			S3CompatibleEndpoint: TestS3RouteHost,
			S3Region:             "",
			S3SecretRef: corev1.SecretReference{
				Name: CreateUniqueSecretName(sourceManagedClusterName, StorageClusterNamespace, StorageClusterName, S3ProfilePrefix),
			},
		},
		{
			S3ProfileName:        fmt.Sprintf("%s-%s-%s", S3ProfilePrefix, destinationManagedClusterName, StorageClusterName),
			S3Bucket:             TestS3BucketName,
			S3CompatibleEndpoint: TestS3RouteHost,
			S3Region:             "",
			S3SecretRef: corev1.SecretReference{
				Name: CreateUniqueSecretName(destinationManagedClusterName, StorageClusterNamespace, StorageClusterName, S3ProfilePrefix),
			},
		},
	}
}

func expectedRamenConfig(ramenNamespace string, source, destination string) rmn.RamenConfig {
	return rmn.RamenConfig{
		S3StoreProfiles: getS3Profile(ramenNamespace, source, destination),
	}
}

func expectedRamenSecretData() map[string][]byte {
	return map[string][]byte{
		AwsAccessKeyId:     []byte(TestAwsAccessKeyId),
		AwsSecretAccessKey: []byte(TestAwssecretaccesskey),
	}
}

func TestCreateOrUpdateSecretsFromInternalSecret(t *testing.T) {
	cases := []struct {
		name            string
		ramenNamespace  string
		manageS3        bool
		ignoreS3Profile bool
	}{
		{
			name:            "Managing S3 Profile disabled",
			ramenNamespace:  ramenNamespace,
			manageS3:        false,
			ignoreS3Profile: true,
		},
		{
			name:            "Creating new S3 Profile in empty Ramen Config",
			ramenNamespace:  ramenNamespace,
			manageS3:        true,
			ignoreS3Profile: false,
		},
		{
			name:            "Creating new S3 Profile in filled Ramen Config",
			ramenNamespace:  "namespace1",
			manageS3:        true,
			ignoreS3Profile: false,
		},
		{
			name:            "Updating existing S3 Profile in filled Ramen Config",
			ramenNamespace:  "namespace2",
			manageS3:        true,
			ignoreS3Profile: false,
		},
	}

	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	assert.NoError(t, err)
	err = multiclusterv1alpha1.AddToScheme(scheme)
	assert.NoError(t, err)
	fakeClient := getFakeClient(t, scheme)
	fakeLogger := GetLogger(GetZapLogger(true))
	for _, c := range cases {
		os.Setenv("POD_NAMESPACE", c.ramenNamespace)
		ctx := context.TODO()
		err := CreateOrUpdateSecretsFromInternalSecret(ctx, fakeClient, scheme, c.ramenNamespace, fakeS3InternalSecret(t, TestSourceManagedClusterEast), fakeMirrorPeers(c.manageS3), fakeLogger)
		assert.NoError(t, err)
		err = CreateOrUpdateSecretsFromInternalSecret(ctx, fakeClient, scheme, c.ramenNamespace, fakeS3InternalSecret(t, TestDestinationManagedClusterWest), fakeMirrorPeers(c.manageS3), fakeLogger)
		assert.NoError(t, err)

		if c.ignoreS3Profile {
			// assert ramen config map update
			ramenConfig := getRamenConfig(t, ctx, fakeClient, c.ramenNamespace)
			assert.True(t, reflect.DeepEqual(rmn.RamenConfig{S3StoreProfiles: []rmn.S3StoreProfile(nil)}, ramenConfig))
		} else {
			// assert ramen config map update
			ramenConfig := getRamenConfig(t, ctx, fakeClient, c.ramenNamespace)
			expectedConfig := expectedRamenConfig(c.ramenNamespace, TestSourceManagedClusterEast, TestDestinationManagedClusterWest)
			if c.name == "Creating new S3 Profile in filled Ramen Config" {
				expectedConfig.S3StoreProfiles = append(expectedRamenConfig(c.ramenNamespace, TestSourceManagedClusterSoth, TestDestinationManagedClusterNorth).S3StoreProfiles, expectedConfig.S3StoreProfiles...)
			}
			assert.True(t, reflect.DeepEqual(expectedConfig, ramenConfig))

			// asset ramen s3 secret creation
			ramenSecret, err := getRamenS3Secret(TestSourceManagedClusterEast, ctx, fakeClient, c.ramenNamespace)
			assert.NoError(t, err)
			assert.True(t, reflect.DeepEqual(expectedRamenSecretData(), ramenSecret.Data))

			ramenSecret, err = getRamenS3Secret(TestDestinationManagedClusterWest, ctx, fakeClient, c.ramenNamespace)
			assert.NoError(t, err)
			assert.True(t, reflect.DeepEqual(expectedRamenSecretData(), ramenSecret.Data))
		}
	}
}
