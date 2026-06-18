package utils

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetOpenShiftVersion(t *testing.T) {
	scheme := runtime.NewScheme()
	err := configv1.AddToScheme(scheme)
	assert.NoError(t, err)

	cases := []struct {
		name            string
		clusterVersion  *configv1.ClusterVersion
		expectedVersion string
		errExpected     bool
	}{
		{
			name: "returns completed version from history",
			clusterVersion: &configv1.ClusterVersion{
				ObjectMeta: metav1.ObjectMeta{Name: "version"},
				Status: configv1.ClusterVersionStatus{
					History: []configv1.UpdateHistory{
						{State: configv1.CompletedUpdate, Version: "4.22.0-0.nightly-2026-06-14-141125"},
					},
				},
			},
			expectedVersion: "4.22.0-0.nightly-2026-06-14-141125",
			errExpected:     false,
		},
		{
			name: "returns first completed version when upgrade is in progress",
			clusterVersion: &configv1.ClusterVersion{
				ObjectMeta: metav1.ObjectMeta{Name: "version"},
				Status: configv1.ClusterVersionStatus{
					History: []configv1.UpdateHistory{
						{State: configv1.PartialUpdate, Version: "4.22.0-0.nightly-2026-06-14-141125"},
						{State: configv1.CompletedUpdate, Version: "4.21.3"},
					},
				},
			},
			expectedVersion: "4.21.3",
			errExpected:     false,
		},
		{
			name: "returns error when no completed version in history",
			clusterVersion: &configv1.ClusterVersion{
				ObjectMeta: metav1.ObjectMeta{Name: "version"},
				Status: configv1.ClusterVersionStatus{
					History: []configv1.UpdateHistory{
						{State: configv1.PartialUpdate, Version: "4.22.0"},
					},
				},
			},
			expectedVersion: "",
			errExpected:     true,
		},
		{
			name:            "returns error when clusterversion object not found",
			clusterVersion:  nil,
			expectedVersion: "",
			errExpected:     true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if c.clusterVersion != nil {
				builder = builder.WithObjects(c.clusterVersion)
			}
			client := builder.Build()

			version, err := GetOpenShiftVersion(context.TODO(), client)
			if c.errExpected {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, c.expectedVersion, version)
			}
		})
	}
}
