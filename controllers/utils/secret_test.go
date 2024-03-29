package utils

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	// Constitutes the cluster-peer-token and final exchanged secret
	rookSecret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "b91e4ac0fd672577e6c1547441440c48935ae20",
			Namespace: "openshift-storage",
		},
		Data: map[string][]byte{
			"token":   []byte("eyJmc2lkIjoiMzU2NjZlNGMtZTljMC00ZmE3LWE3MWEtMmIwNTJiZjUxOTFhIiwiY2xpZW50X2lkIjoicmJkLW1pcnJvci1wZWVyIiwia2V5IjoiQVFDZVkwNWlYUmtsTVJBQU95b3I3ZTZPL3MrcTlzRnZWcVpVaHc9PSIsIm1vbl9ob3N0IjoiMTcyLjMxLjE2NS4yMjg6Njc4OSwxNzIuMzEuMTkxLjE0MDo2Nzg5LDE3Mi4zMS44LjQ0OjY3ODkiLCJuYW1lc3BhY2UiOiJvcGVuc2hpZnQtc3RvcmFnZSJ9"),
			"cluster": []byte("ocs-storagecluster-cephcluster"),
		},
	}

	// Constitutes both blue secret and green secret present on the hub
	hubSecret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "d8433b8cb5b6d99c4d785ebd6082efd19cad50c",
			Namespace: "openshift-storage",
		},
		Data: map[string][]byte{
			"namespace":            []byte("openshift-storage"),
			"secret-data":          []byte(`{"cluster":"b2NzLXN0b3JhZ2VjbHVzdGVyLWNlcGhjbHVzdGVy","token":"ZXlKbWMybGtJam9pWXpSak56SmpNRE10WXpCbFlpMDBZMlppTFRnME16RXRNekExTmpZME16UmxZV1ZqSWl3aVkyeHBaVzUwWDJsa0lqb2ljbUprTFcxcGNuSnZjaTF3WldWeUlpd2lhMlY1SWpvaVFWRkVkbGxyTldrM04xbG9TMEpCUVZZM2NFZHlVVXBrU1VvelJtZGpjVWxGVUZWS0wzYzlQU0lzSW0xdmJsOW9iM04wSWpvaU1UY3lMak13TGpFd01TNHlORGs2TmpjNE9Td3hOekl1TXpBdU1UZ3pMakU1TURvMk56ZzVMREUzTWk0ek1DNHlNak11TWpFd09qWTNPRGtpTENKdVlXMWxjM0JoWTJVaU9pSnZjR1Z1YzJocFpuUXRjM1J2Y21GblpTSjk="}`),
			"secret-origin":        []byte("rook"),
			"storage-cluster-name": []byte("ocs-storagecluster"),
		},
		Type: "multicluster.odf.openshift.io/secret-type",
	}

	// Constitutes the s3 secret synced on hub
	s3Secret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "10d0befe9022a438cb7216391c32e6eba32f19f",
			Namespace: "local-cluster",
		},
		Data: map[string][]byte{
			"namespace":            []byte("openshift-storage"),
			"secret-data":          []byte(`{"AWS_ACCESS_KEY_ID":"dXNlcjEyMzQ=","AWS_SECRET_ACCESS_KEY":"cGFzc3dvcmQxMjM0","s3Bucket":"b2RyYnVja2V0LWJjZjMwNDFmMjFkNw==","s3CompatibleEndpoint":"aHR0cHM6Ly9zMy1vcGVuc2hpZnQtc3RvcmFnZS5hcHBzLmh1Yi01MTY3NjNiMC0yZjQzLTRmMGYtYWI3Zi0wYzI4YjYzM2FjMTAuZGV2Y2x1c3Rlci5vcGVuc2hpZnQuY29t","s3ProfileName":"czNwcm9maWxlLWxvY2FsLWNsdXN0ZXItb2NzLXN0b3JhZ2VjbHVzdGVy","s3Region":"bm9vYmFh"}`),
			"secret-origin":        []byte("S3"),
			"storage-cluster-name": []byte("ocs-storagecluster"),
		},
	}
)

func TestUnmarshalRookSecret(t *testing.T) {
	testCases := []struct {
		input          *corev1.Secret
		expectedOutput *RookToken
	}{
		{&rookSecret, &RookToken{FSID: "35666e4c-e9c0-4fa7-a71a-2b052bf5191a", Namespace: "openshift-storage", MonHost: "172.31.165.228:6789,172.31.191.140:6789,172.31.8.44:6789", ClientId: "rbd-mirror-peer", Key: "AQCeY05iXRklMRAAOyor7e6O/s+q9sFvVqZUhw=="}},
	}

	for _, testCase := range testCases {
		actualOutput, err := UnmarshalRookSecret(testCase.input)
		if err != nil {
			t.Errorf("TestUnmarshalRookSecret() failed. Error: %s", err)
		}

		if actualOutput.FSID != testCase.expectedOutput.FSID {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.FSID, actualOutput.FSID)
		}

		if actualOutput.Namespace != testCase.expectedOutput.Namespace {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.Namespace, actualOutput.Namespace)
		}

		if actualOutput.MonHost != testCase.expectedOutput.MonHost {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.MonHost, actualOutput.MonHost)
		}

		if actualOutput.ClientId != testCase.expectedOutput.ClientId {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.ClientId, actualOutput.ClientId)
		}

		if actualOutput.Key != testCase.expectedOutput.Key {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.Key, actualOutput.Key)
		}
	}
}

func TestUnmarshalHubSecret(t *testing.T) {
	testCases := []struct {
		input          *corev1.Secret
		expectedOutput *RookToken
	}{
		{&hubSecret, &RookToken{
			FSID:      "c4c72c03-c0eb-4cfb-8431-30566434eaec",
			Namespace: "openshift-storage",
			MonHost:   "172.30.101.249:6789,172.30.183.190:6789,172.30.223.210:6789",
			ClientId:  "rbd-mirror-peer",
			Key:       "AQDvYk5i77YhKBAAV7pGrQJdIJ3FgcqIEPUJ/w==",
		}},
	}

	for _, testCase := range testCases {
		actualOutput, err := UnmarshalHubSecret(testCase.input)
		if err != nil {
			t.Errorf("TestUnmarshalHubSecret() failed. Error: %s", err)
		}

		if actualOutput.FSID != testCase.expectedOutput.FSID {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.FSID, actualOutput.FSID)
		}

		if actualOutput.Namespace != testCase.expectedOutput.Namespace {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.Namespace, actualOutput.Namespace)
		}

		if actualOutput.MonHost != testCase.expectedOutput.MonHost {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.MonHost, actualOutput.MonHost)
		}

		if actualOutput.ClientId != testCase.expectedOutput.ClientId {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.ClientId, actualOutput.ClientId)
		}

		if actualOutput.Key != testCase.expectedOutput.Key {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.Key, actualOutput.Key)
		}
	}
}
func TestUnmarshalS3Secret(t *testing.T) {
	testCases := []struct {
		input          *corev1.Secret
		expectedOutput *S3Token
	}{
		{&s3Secret, &S3Token{AccessKeyID: "user1234", SecretAccessKey: "password1234", S3Bucket: "odrbucket-bcf3041f21d7", S3CompatibleEndpoint: "https://s3-openshift-storage.apps.hub-516763b0-2f43-4f0f-ab7f-0c28b633ac10.devcluster.openshift.com", S3ProfileName: "s3profile-local-cluster-ocs-storagecluster", S3Region: "noobaa"}},
	}

	for _, testCase := range testCases {
		actualOutput, err := UnmarshalS3Secret(testCase.input)
		if err != nil {
			t.Errorf("TestUnmarshalS3Secret() failed. Error: %s", err)
		}

		if actualOutput.AccessKeyID != testCase.expectedOutput.AccessKeyID {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.AccessKeyID, actualOutput.AccessKeyID)
		}

		if actualOutput.SecretAccessKey != testCase.expectedOutput.SecretAccessKey {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.SecretAccessKey, actualOutput.SecretAccessKey)
		}

		if actualOutput.S3Bucket != testCase.expectedOutput.S3Bucket {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.S3Bucket, actualOutput.S3Bucket)
		}

		if actualOutput.S3CompatibleEndpoint != testCase.expectedOutput.S3CompatibleEndpoint {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.S3CompatibleEndpoint, actualOutput.S3CompatibleEndpoint)
		}

		if actualOutput.S3ProfileName != testCase.expectedOutput.S3ProfileName {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.S3ProfileName, actualOutput.S3ProfileName)
		}

		if actualOutput.S3Region != testCase.expectedOutput.S3Region {
			t.Errorf("Expected %s, received value %s", testCase.expectedOutput.S3Region, actualOutput.S3Region)
		}
	}
}

var externalSecret = corev1.Secret{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "10d0befe9022a438cb7216391c32e6eba32f19f",
		Namespace: "openshift-storage",
	},
	Data: map[string][]byte{
		"ceph-username": []byte("user1234"),
		"mon-secret":    []byte("mon-secret-data"),
		"ceph-secret":   []byte("ceph-secret-data"),
		"fsid":          []byte("c4c72c03-c0eb-4cfb-8431-30566434eaed"),
	},
}

func TestUnmarshalRookSecretExternal(t *testing.T) {

	tests := []struct {
		name           string
		input          *corev1.Secret
		expectedOutput *RookTokenExternal
	}{
		{
			"rook-ceph-mon secret test",
			&externalSecret,
			&RookTokenExternal{
				CephSecret:   "ceph-secret-data",
				CephUsername: "user1234",
				FSID:         "c4c72c03-c0eb-4cfb-8431-30566434eaed",
				MonSecret:    "mon-secret-data",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalRookSecretExternal(tt.input)
			if err != nil {
				t.Errorf("UnmarshalRookSecretExternal() error = %v", err)
				return
			}
			if !reflect.DeepEqual(got, tt.expectedOutput) {
				t.Errorf("UnmarshalRookSecretExternal() got = %v, want %v", got, tt.expectedOutput)
			}
		})
	}
}
