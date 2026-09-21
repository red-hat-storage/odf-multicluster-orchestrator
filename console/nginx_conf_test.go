package console

import (
	"strings"
	"testing"

	ocstlsv1 "github.com/red-hat-storage/ocs-tls-profiles/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateNginxConf_NilConfig(t *testing.T) {
	conf, err := GenerateNginxConf(nil)
	require.NoError(t, err)
	assert.Contains(t, conf, "listen       9001 ssl;")
	assert.Contains(t, conf, "ssl_certificate /var/serving-cert/tls.crt;")
	assert.Contains(t, conf, "worker_processes 8;")
	assert.NotContains(t, conf, "ssl_protocols")
	assert.NotContains(t, conf, "ssl_ciphers")
	assert.NotContains(t, conf, "ssl_conf_command")
}

func TestGenerateNginxConf_TLS13(t *testing.T) {
	ossl := &ocstlsv1.OpenSSLConfig{
		Protocol: "TLSv1.3",
		Ciphers:  []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384"},
		Groups:   []string{"X25519MLKEM768", "prime256v1"},
	}
	conf, err := GenerateNginxConf(ossl)
	require.NoError(t, err)
	assert.Contains(t, conf, "ssl_protocols TLSv1.3;")
	assert.Contains(t, conf, "ssl_conf_command Ciphersuites TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384;")
	assert.NotContains(t, conf, "ssl_ciphers")
	assert.Contains(t, conf, "ssl_conf_command Groups X25519MLKEM768:prime256v1;")
}

func TestGenerateNginxConf_TLS12(t *testing.T) {
	ossl := &ocstlsv1.OpenSSLConfig{
		Protocol: "TLSv1.2",
		Ciphers:  []string{"ECDHE-RSA-AES128-GCM-SHA256", "ECDHE-RSA-AES256-GCM-SHA384"},
		Groups:   []string{"prime256v1", "secp384r1"},
	}
	conf, err := GenerateNginxConf(ossl)
	require.NoError(t, err)
	assert.Contains(t, conf, "ssl_protocols TLSv1.2;")
	assert.Contains(t, conf, "ssl_ciphers ECDHE-RSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-GCM-SHA384;")
	assert.NotContains(t, conf, "ssl_conf_command Ciphersuites")
	assert.Contains(t, conf, "ssl_conf_command Groups prime256v1:secp384r1;")
}

func TestGenerateNginxConf_EmptyGroups(t *testing.T) {
	ossl := &ocstlsv1.OpenSSLConfig{
		Protocol: "TLSv1.3",
		Ciphers:  []string{"TLS_AES_128_GCM_SHA256"},
		Groups:   nil,
	}
	conf, err := GenerateNginxConf(ossl)
	require.NoError(t, err)
	assert.Contains(t, conf, "ssl_protocols TLSv1.3;")
	assert.Contains(t, conf, "ssl_conf_command Ciphersuites TLS_AES_128_GCM_SHA256;")
	assert.NotContains(t, conf, "ssl_ciphers")
	assert.NotContains(t, conf, "ssl_conf_command Groups")
}

func TestGenerateNginxConf_ValidNginxStructure(t *testing.T) {
	conf, err := GenerateNginxConf(nil)
	require.NoError(t, err)
	assert.True(t, strings.Contains(conf, "worker_processes"))
	assert.True(t, strings.Contains(conf, "http {"))
	assert.True(t, strings.Contains(conf, "server {"))
	assert.True(t, strings.Contains(conf, "location /"))
	assert.True(t, strings.Contains(conf, "location /compatibility/"))
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
	conf, err := GenerateNginxConf(nil)
	require.NoError(t, err)
	assert.Contains(t, conf, "worker_processes 2;")
	assert.NotContains(t, conf, "worker_processes auto;")
}

func TestGetNginxConfConfigMap(t *testing.T) {
	cm := GetNginxConfConfigMap("test-namespace", "test-conf-content")
	assert.Equal(t, NginxConfigMapName, cm.Name)
	assert.Equal(t, "test-namespace", cm.Namespace)
	assert.Equal(t, "test-conf-content", cm.Data[NginxConfKey])
	assert.Equal(t, PluginName, cm.Labels["app.kubernetes.io/name"])
}
