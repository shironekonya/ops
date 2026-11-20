package main

// 本文件实现 TODO 8：配置文件。
//
// 数据流：config.json --LoadConfig--> Config --main--> HTTP Server / 刷新循环 / 日志
//
// 设计约定：
//   - 选 JSON 而不是 YAML：标准库 encoding/json 即可，不引第三方依赖；
//   - 文件不存在（未配置过）→ 全部用默认值，首次部署零配置可跑；
//   - 文件存在但解析失败 / 字段非法 → 致命错误，拒绝启动。
//     带病运行（比如刷新间隔被写成 0）比不启动更危险；
//   - 文件里没写的字段 → 用默认值，支持只覆盖关心的项；
//   - 不引入 Viper 之类的配置框架，四个字段用不上。

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// 默认值集中定义，LoadConfig 和文档共用这一份。
const (
	defaultListenAddr      = ":9105"
	defaultResultDir       = "data"
	defaultRefreshInterval = 10 * time.Second
	defaultLogLevel        = "info"
)

// Config 是加载并校验后的运行时配置，字段全部为强类型。
type Config struct {
	ListenAddr      string        // HTTP 监听地址，如 ":9105"
	ResultDir       string        // 任务结果目录
	RefreshInterval time.Duration // 指标刷新间隔
	LogLevel        slog.Level    // 日志级别（debug/info/warn/error）
}

// configFile 是 JSON 文件的原始形态，字段全是字符串，
// 解析成 Config 之前逐项转换和校验，错误信息能精确指到字段。
type configFile struct {
	ListenAddr      string `json:"listen_addr"`
	ResultDir       string `json:"result_dir"`
	RefreshInterval string `json:"refresh_interval"`
	LogLevel        string `json:"log_level"`
}

// LoadConfig 从 path 加载配置。
// path 为空或文件不存在时返回纯默认值；
// 文件内容非法时返回带字段名的错误。
func LoadConfig(path string) (Config, error) {
	cfg := Config{
		ListenAddr:      defaultListenAddr,
		ResultDir:       defaultResultDir,
		RefreshInterval: defaultRefreshInterval,
		LogLevel:        slog.LevelInfo,
	}

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// 没有配置文件不算错误：首次部署允许零配置启动。
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	var f configFile
	if err := json.Unmarshal(data, &f); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}

	if f.ListenAddr != "" {
		cfg.ListenAddr = f.ListenAddr
	}
	if f.ResultDir != "" {
		cfg.ResultDir = f.ResultDir
	}
	if f.RefreshInterval != "" {
		d, err := time.ParseDuration(f.RefreshInterval)
		if err != nil {
			return Config{}, fmt.Errorf("config %q: invalid refresh_interval %q: %w", path, f.RefreshInterval, err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("config %q: refresh_interval must be positive, got %q", path, f.RefreshInterval)
		}
		cfg.RefreshInterval = d
	}
	if f.LogLevel != "" {
		// slog.Level 自带 UnmarshalText，接受 debug/info/warn/error（大小写不敏感）。
		var level slog.Level
		if err := level.UnmarshalText([]byte(f.LogLevel)); err != nil {
			return Config{}, fmt.Errorf("config %q: invalid log_level %q: %w", path, f.LogLevel, err)
		}
		cfg.LogLevel = level
	}

	return cfg, nil
}
