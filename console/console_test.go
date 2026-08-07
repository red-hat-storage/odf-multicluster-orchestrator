package console

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
)

func TestGetNetworkPolicy(t *testing.T) {
	policy := GetNetworkPolicy("openshift-operators")

	assert.Equal(t, PluginName, policy.Name)
	assert.Equal(t, "openshift-operators", policy.Namespace)
	assert.Equal(t, map[string]string{serviceLabelKey: PluginName}, policy.Spec.PodSelector.MatchLabels)
	assert.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, policy.Spec.PolicyTypes)
	require.Len(t, policy.Spec.Ingress, 1)
	require.Len(t, policy.Spec.Ingress[0].From, 1)
	peer := policy.Spec.Ingress[0].From[0]
	assert.Equal(t, map[string]string{"kubernetes.io/metadata.name": "openshift-console"}, peer.NamespaceSelector.MatchLabels)
	assert.Equal(t, map[string]string{"app": "console", "component": "ui"}, peer.PodSelector.MatchLabels)
	require.Len(t, policy.Spec.Ingress[0].Ports, 1)
	require.NotNil(t, policy.Spec.Ingress[0].Ports[0].Protocol)
	require.NotNil(t, policy.Spec.Ingress[0].Ports[0].Port)
	assert.Equal(t, "TCP", string(*policy.Spec.Ingress[0].Ports[0].Protocol))
	assert.Equal(t, int32(9001), policy.Spec.Ingress[0].Ports[0].Port.IntVal)
	assert.NotNil(t, policy.Spec.Egress)
	assert.Empty(t, policy.Spec.Egress)
}

func TestGetBasePath(t *testing.T) {
	cases := []struct {
		name             string
		clusterVersion   string
		expectedBasePath string
	}{
		{
			name:             "OCP 4.21 should use main base path",
			clusterVersion:   "4.21.3",
			expectedBasePath: MAIN_BASE_PATH,
		},
		{
			name:             "OCP 4.20 should use main base path",
			clusterVersion:   "4.20.5",
			expectedBasePath: MAIN_BASE_PATH,
		},
		{
			name:             "OCP 4.22 should use main base path",
			clusterVersion:   "4.22.0",
			expectedBasePath: MAIN_BASE_PATH,
		},
		{
			name:             "OCP 4.23 should use compatibility base path",
			clusterVersion:   "4.23.0",
			expectedBasePath: COMPATIBILITY_BASE_PATH,
		},
		{
			name:             "OCP 4.23 nightly should use compatibility base path",
			clusterVersion:   "4.23.0-0.nightly-2026-05-28-111510",
			expectedBasePath: COMPATIBILITY_BASE_PATH,
		},
		{
			name:             "OCP 5.0 should use compatibility base path",
			clusterVersion:   "5.0.1",
			expectedBasePath: COMPATIBILITY_BASE_PATH,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := GetBasePath(c.clusterVersion)
			assert.Equal(t, c.expectedBasePath, result)
		})
	}
}
