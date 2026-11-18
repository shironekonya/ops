// config_test.go：参数解析与 kubeconfig 加载的单元测试（不依赖真实集群）。
package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	var stderr bytes.Buffer
	cfg, err := Parse([]string{
		"-kubeconfig", "/tmp/k3s.yaml",
		"-namespace", "kube-system",
		"-timeout", "5s",
	}, &stderr)
	if err != nil {
		t.Fatalf("解析参数失败: %v", err)
	}
	if cfg.Kubeconfig != "/tmp/k3s.yaml" || cfg.Namespace != "kube-system" || cfg.Timeout != 5*time.Second {
		t.Fatalf("参数解析结果不正确: %+v", cfg)
	}
}

func TestParseDefaults(t *testing.T) {
	var stderr bytes.Buffer
	cfg, err := Parse([]string{}, &stderr)
	if err != nil {
		t.Fatalf("空参数应当使用默认值: %v", err)
	}
	if cfg.Kubeconfig != "" || cfg.Namespace != "" || cfg.Timeout != 10*time.Second {
		t.Fatalf("默认值不正确: %+v", cfg)
	}
}

func TestParseUnknownFlag(t *testing.T) {
	var stderr bytes.Buffer
	if _, err := Parse([]string{"-no-such-flag"}, &stderr); err == nil {
		t.Fatal("未知参数应当返回错误")
	}
}

func TestParseOutput(t *testing.T) {
	testCases := []struct {
		name string
		args []string
		want OutputFormat
	}{
		{name: "默认 text", args: []string{}, want: OutputText},
		{name: "显式 text", args: []string{"-output", "text"}, want: OutputText},
		{name: "显式 json", args: []string{"-output", "json"}, want: OutputJSON},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			cfg, err := Parse(tc.args, &stderr)
			if err != nil {
				t.Fatalf("解析参数失败: %v", err)
			}
			if cfg.Output != tc.want {
				t.Fatalf("Output = %q, want %q", cfg.Output, tc.want)
			}
		})
	}
}

func TestParseOutputInvalid(t *testing.T) {
	var stderr bytes.Buffer
	if _, err := Parse([]string{"-output", "yaml"}, &stderr); err == nil {
		t.Fatal("不支持的输出格式应当返回错误")
	} else if !strings.Contains(err.Error(), "不支持的输出格式") {
		t.Fatalf("错误信息缺少上下文: %v", err)
	}
}

func TestBuildRESTConfigMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-exist.yaml")
	if _, err := BuildRESTConfig(missing); err == nil {
		t.Fatal("kubeconfig 文件不存在时应当返回错误")
	} else if !strings.Contains(err.Error(), "读取 kubeconfig") {
		t.Fatalf("错误信息缺少上下文: %v", err)
	}
}

func TestBuildRESTConfigFromFile(t *testing.T) {
	kubeconfig := `apiVersion: v1
kind: Config
clusters:
- name: demo
  cluster:
    server: https://127.0.0.1:6443
    insecure-skip-tls-verify: true
contexts:
- name: demo
  context:
    cluster: demo
    user: demo-user
current-context: demo
users:
- name: demo-user
  user:
    token: fake-token
`
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
		t.Fatalf("写入临时 kubeconfig 失败: %v", err)
	}

	restConfig, err := BuildRESTConfig(path)
	if err != nil {
		t.Fatalf("解析 kubeconfig 失败: %v", err)
	}
	if restConfig.Host != "https://127.0.0.1:6443" {
		t.Fatalf("Host = %q, want https://127.0.0.1:6443", restConfig.Host)
	}
}
