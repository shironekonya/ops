package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfig 在临时目录写一个配置文件，返回完整路径。
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestLoadConfig_MissingFile 验收：文件不存在不算错误，返回纯默认值。
// 首次部署允许零配置启动。
func TestLoadConfig_MissingFile(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not be an error: %v", err)
	}
	if cfg.ListenAddr != defaultListenAddr || cfg.ResultDir != defaultResultDir {
		t.Errorf("defaults not applied: %+v", cfg)
	}
	if cfg.RefreshInterval != defaultRefreshInterval {
		t.Errorf("RefreshInterval = %v, want %v", cfg.RefreshInterval, defaultRefreshInterval)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
}

// TestLoadConfig_EmptyPath 验证不指定配置文件时直接返回默认值。
func TestLoadConfig_EmptyPath(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != defaultListenAddr {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, defaultListenAddr)
	}
}

// TestLoadConfig_Full 验证四项配置全部生效。
func TestLoadConfig_Full(t *testing.T) {
	path := writeConfig(t, `{
		"listen_addr": ":9999",
		"result_dir": "/var/ops-results",
		"refresh_interval": "30s",
		"log_level": "debug"
	}`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":9999" {
		t.Errorf("ListenAddr = %q, want :9999", cfg.ListenAddr)
	}
	if cfg.ResultDir != "/var/ops-results" {
		t.Errorf("ResultDir = %q", cfg.ResultDir)
	}
	if cfg.RefreshInterval != 30*time.Second {
		t.Errorf("RefreshInterval = %v, want 30s", cfg.RefreshInterval)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug", cfg.LogLevel)
	}
}

// TestLoadConfig_Partial 验证部分配置：没写的字段回落到默认值。
func TestLoadConfig_Partial(t *testing.T) {
	path := writeConfig(t, `{"refresh_interval": "1m"}`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RefreshInterval != time.Minute {
		t.Errorf("RefreshInterval = %v, want 1m", cfg.RefreshInterval)
	}
	if cfg.ListenAddr != defaultListenAddr {
		t.Errorf("ListenAddr = %q, want default %q", cfg.ListenAddr, defaultListenAddr)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want default info", cfg.LogLevel)
	}
}

// TestLoadConfig_LevelCaseInsensitive 验证日志级别大小写不敏感。
func TestLoadConfig_LevelCaseInsensitive(t *testing.T) {
	path := writeConfig(t, `{"log_level": "WARN"}`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LogLevel != slog.LevelWarn {
		t.Errorf("LogLevel = %v, want warn", cfg.LogLevel)
	}
}

// TestLoadConfig_Invalid 集中覆盖非法配置：必须报错且错误能定位到字段。
func TestLoadConfig_Invalid(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantErr string
	}{
		{"JSON 语法错误", `{bad json`, "parse config"},
		{"间隔格式错误", `{"refresh_interval": "abc"}`, "invalid refresh_interval"},
		{"间隔为零", `{"refresh_interval": "0s"}`, "refresh_interval must be positive"},
		{"间隔为负", `{"refresh_interval": "-5s"}`, "refresh_interval must be positive"},
		{"日志级别非法", `{"log_level": "loud"}`, "invalid log_level"},
		{"字段类型错误", `{"refresh_interval": 30}`, "parse config"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, tc.content)
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
