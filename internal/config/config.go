package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port               int
	DBPath             string
	RedisAddr          string
	RedisPass          string
	RustFSEndpoint     string
	RustFSBucket       string
	RustFSRegion       string
	RustFSAccessKey    string
	RustFSSecretKey    string
	LogLevel           string
	ArtifactTTLDays    int
	MaxUploadSizeMB    int
}

func Load() *Config {
	return &Config{
		Port:               getEnvInt("ACB_PORT", 8080),
		DBPath:             getEnv("ACB_DB_PATH", "/var/lib/acb/acb.db"),
		RedisAddr:          getEnv("ACB_REDIS_ADDR", "localhost:6379"),
		RedisPass:          getEnv("ACB_REDIS_PASS", ""),
		RustFSEndpoint:     getEnv("ACB_RUSTFS_ENDPOINT", "localhost:8085"),
		RustFSBucket:       getEnv("ACB_RUSTFS_BUCKET", "acb-artifacts"),
		RustFSRegion:       getEnv("RUSTFS_REGION", "us-east-1"),
		RustFSAccessKey:    getEnv("RUSTFS_ACCESS_KEY_ID", ""),
		RustFSSecretKey:    getEnv("RUSTFS_SECRET_ACCESS_KEY", ""),
		LogLevel:           getEnv("ACB_LOG_LEVEL", "info"),
		ArtifactTTLDays:    getEnvInt("ACB_ARTIFACT_TTL_DAYS", 30),
		MaxUploadSizeMB:    getEnvInt("ACB_MAX_UPLOAD_SIZE_MB", 32),
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
