package console

import (
	"bytes"
	"strings"
	"text/template"

	ocstlsv1 "github.com/red-hat-storage/ocs-tls-profiles/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	NginxConfigMapName = "odf-multicluster-console-nginx-conf"
	NginxConfKey       = "nginx.conf"
)

type nginxTLSConfig struct {
	Protocol string
	Ciphers  string
	Groups   string
	IsTLS13  bool
}

var nginxConfTemplate = template.Must(template.New("nginx.conf").Parse(`worker_processes auto;
error_log /var/log/nginx/error.log;
pid /var/lib/nginx/tmp/nginx.pid;

include /usr/share/nginx/modules/*.conf;

events {
    worker_connections 1024;
}

http {
    client_body_temp_path /var/lib/nginx/tmp/client_temp;
    proxy_temp_path       /var/lib/nginx/tmp/proxy_temp_path;
    fastcgi_temp_path     /var/lib/nginx/tmp/fastcgi_temp;
    uwsgi_temp_path       /var/lib/nginx/tmp/uwsgi_temp;
    scgi_temp_path        /var/lib/nginx/tmp/scgi_temp;

    log_format  main  '$remote_addr - $remote_user [$time_local] "$request" '
                      '$status $body_bytes_sent "$http_referer" '
                      '"$http_user_agent" "$http_x_forwarded_for"';

    access_log  /var/log/nginx/access.log  main;

    sendfile            on;
    tcp_nopush          on;
    tcp_nodelay         on;
    keepalive_timeout   65;
    types_hash_max_size 4096;

    include             /etc/nginx/mime.types;
    default_type        application/octet-stream;

    server {
        listen       9001 ssl;
        listen       [::]:9001 ssl;
        ssl_certificate /var/serving-cert/tls.crt;
        ssl_certificate_key /var/serving-cert/tls.key;
{{- if .Protocol }}
        ssl_protocols {{ .Protocol }};
{{- end }}
{{- if .Ciphers }}
{{- if .IsTLS13 }}
        ssl_conf_command Ciphersuites {{ .Ciphers }};
{{- else }}
        ssl_ciphers {{ .Ciphers }};
{{- end }}
{{- end }}
{{- if .Groups }}
        ssl_conf_command Groups {{ .Groups }};
{{- end }}

        location / {
            root   /opt/app-root/src;
        }
        location /compatibility/ {
            root   /opt/app-root/src;
        }
        error_page   500 502 503 504  /50x.html;
        location = /50x.html {
            root   /usr/share/nginx/html;
        }
        ssi on;
        add_header Last-Modified $date_gmt;
        add_header Cache-Control 'no-store, no-cache, must-revalidate, proxy-revalidate, max-age=0';
        if_modified_since off;
        expires off;
        etag off;
    }

}
`))

func GenerateNginxConf(ossl *ocstlsv1.OpenSSLConfig) (string, error) {
	var cfg nginxTLSConfig
	if ossl != nil {
		cfg.Protocol = ossl.Protocol
		cfg.IsTLS13 = ossl.Protocol == "TLSv1.3"
		if len(ossl.Ciphers) > 0 {
			cfg.Ciphers = strings.Join(ossl.Ciphers, ":")
		}
		if len(ossl.Groups) > 0 {
			cfg.Groups = strings.Join(ossl.Groups, ":")
		}
	}

	var buf bytes.Buffer
	if err := nginxConfTemplate.Execute(&buf, cfg); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func GetNginxConfConfigMap(namespace, nginxConf string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      NginxConfigMapName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name": PluginName,
			},
		},
		Data: map[string]string{
			NginxConfKey: nginxConf,
		},
	}
}
