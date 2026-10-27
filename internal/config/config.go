// Package config 负责从环境变量加载平台配置，并在启动阶段做安全校验。
package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config 是平台运行期配置。
type Config struct {
	Addr          string
	DataDir       string
	DBPath        string
	LicenseDir    string
	ClockPath     string
	MasterKey     []byte // 原始主密钥，用于派生 AES-256 密钥
	JWTSecret     []byte
	TokenTTL      time.Duration
	AdminUser     string
	AdminPassword string
	Version       string
}

// Load 读取环境变量并创建必要目录。缺失 ULP_MASTER_KEY 时直接失败（不得静默放行）。
func Load(version string) (*Config, error) {
	addr := env("ULP_ADDR", ":7630")
	dataDir := env("ULP_DATA_DIR", "./data")
	if err := os.MkdirAll(filepath.Join(dataDir, "licenses"), 0o700); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	master := strings.TrimSpace(os.Getenv("ULP_MASTER_KEY"))
	if master == "" {
		return nil, fmt.Errorf("缺少环境变量 ULP_MASTER_KEY：私钥加密主密钥不得为空")
	}
	if len(master) < 16 || strings.EqualFold(master, "change-me") {
		return nil, fmt.Errorf("ULP_MASTER_KEY 过弱：至少 16 个字符，且不得使用示例值")
	}

	jwtSecret := strings.TrimSpace(os.Getenv("ULP_JWT_SECRET"))
	if jwtSecret == "" {
		// 未单独提供时由主密钥派生，保证会话密钥不会为空。
		sum := sha256.Sum256([]byte("ULP-JWT:" + master))
		jwtSecret = fmt.Sprintf("%x", sum[:])
	}
	if len(jwtSecret) < 32 {
		return nil, fmt.Errorf("ULP_JWT_SECRET 过弱：至少 32 个字符")
	}

	ttl := 12 * time.Hour
	if raw := strings.TrimSpace(os.Getenv("ULP_TOKEN_TTL")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("ULP_TOKEN_TTL 非法: %q", raw)
		}
		ttl = d
	}

	return &Config{
		Addr:          addr,
		DataDir:       dataDir,
		DBPath:        filepath.Join(dataDir, "ulpd.db"),
		LicenseDir:    filepath.Join(dataDir, "licenses"),
		ClockPath:     filepath.Join(dataDir, "clock.dat"),
		MasterKey:     []byte(master),
		JWTSecret:     []byte(jwtSecret),
		TokenTTL:      ttl,
		AdminUser:     strings.TrimSpace(os.Getenv("ULP_ADMIN_USER")),
		AdminPassword: os.Getenv("ULP_ADMIN_PASSWORD"),
		Version:       version,
	}, nil
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
