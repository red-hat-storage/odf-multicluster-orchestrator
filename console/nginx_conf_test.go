package console

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateNginxConf_DefaultWorkerProcesses(t *testing.T) {
	t.Setenv(NginxWorkerProcessesEnvVar, "")
	conf, err := GenerateNginxConf()
	require.NoError(t, err)
	assert.Contains(t, conf, "worker_processes 8;")
	assert.NotContains(t, conf, "worker_processes auto;")
	assert.Contains(t, conf, "listen       9001 ssl;")
	assert.Contains(t, conf, "ssl_certificate /var/serving-cert/tls.crt;")
}

func TestGetNginxWorkerProcesses(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     string
	}{
		{name: "default when empty", envValue: "", want: DefaultNginxWorkerProcesses},
		{name: "positive integer", envValue: "4", want: "4"},
		{name: "auto", envValue: "auto", want: "auto"},
		{name: "invalid falls back", envValue: "not-a-number", want: DefaultNginxWorkerProcesses},
		{name: "zero falls back", envValue: "0", want: DefaultNginxWorkerProcesses},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(NginxWorkerProcessesEnvVar, tt.envValue)
			assert.Equal(t, tt.want, GetNginxWorkerProcesses())
		})
	}
}

func TestGenerateNginxConf_WorkerProcessesFromEnv(t *testing.T) {
	t.Setenv(NginxWorkerProcessesEnvVar, "2")
	conf, err := GenerateNginxConf()
	require.NoError(t, err)
	assert.Contains(t, conf, "worker_processes 2;")
	assert.NotContains(t, conf, "worker_processes auto;")
}

func TestGenerateNginxConf_ValidNginxStructure(t *testing.T) {
	conf, err := GenerateNginxConf()
	require.NoError(t, err)
	assert.True(t, strings.Contains(conf, "events {"))
	assert.True(t, strings.Contains(conf, "http {"))
	assert.True(t, strings.Contains(conf, "location /compatibility/"))
}

func TestGetNginxConfConfigMap(t *testing.T) {
	cm := GetNginxConfConfigMap("test-namespace", "test-conf-content")
	assert.Equal(t, NginxConfigMapName, cm.Name)
	assert.Equal(t, "test-namespace", cm.Namespace)
	assert.Equal(t, "test-conf-content", cm.Data[NginxConfKey])
}
