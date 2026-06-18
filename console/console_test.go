package console

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
