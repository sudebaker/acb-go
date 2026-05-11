package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port           int
	DBPath         string
	RedisAddr      string
	RedisPass      string
	RustFSEndpoint string
	RustFSBucket   string
	LogLevel       string
}

func Load() *Config {
	return &Config{
		Port:           getEnvInt("ACB_PORT", 8080),
		DBPath:         getEnv("ACB_DB_PATH", "/var/lib/acb/acb.db"),
		RedisAddr:      getEnv("ACB_REDIS_ADDR", "localhost:6379"),
		RedisPass:      getEnv("ACB_REDIS_PASS", ""),
		RustFSEndpoint: getEnv("ACB_RUSTFS_ENDPOINT", "http://localhost:8085"),
		RustFSBucket:   getEnv("ACB_RUSTFS_BUCKET", "acb-artifacts"),
		LogLevel:       getEnv("ACB_LOG_LEVEL", "info"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
