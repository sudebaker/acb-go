package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port                   int
	PGHost                 string
	PGPort                 int
	PGUser                 string
	PGPassword             string
	PGDatabase             string
	RedisAddr              string
	RedisPass              string
	RustFSEndpoint         string
	RustFSBucket           string
	RustFSRegion           string
	RustFSAccessKey        string
	RustFSSecretKey        string
	LogLevel               string
	ArtifactTTLDays        int
	MaxUploadSizeMB        int
	PendingTimeoutMin      int
	PendingTimeoutCheckSec int
	TaskTimeoutMin         int
	TaskTimeoutCheckSec    int
	AgentStaleMin          int
	AgentStaleCheckSec     int
	AllowedSkills          []string
	AllowedTags            []string
}

func Load() *Config {
	return &Config{
		Port:               getEnvInt("ACB_PORT", 8090),
		PGHost:             getEnv("ACB_PG_HOST", "localhost"),
		PGPort:             getEnvInt("ACB_PG_PORT", 5433),
		PGUser:             getEnv("ACB_PG_USER", "acb"),
		PGPassword:         getEnv("ACB_PG_PASSWORD", ""),
		PGDatabase:         getEnv("ACB_PG_DATABASE", "acb"),
		RedisAddr:          getEnv("ACB_REDIS_ADDR", "localhost:6379"),
		RedisPass:          getEnv("ACB_REDIS_PASS", ""),
		RustFSEndpoint:     getEnv("ACB_RUSTFS_ENDPOINT", "localhost:8085"),
		RustFSBucket:       getEnv("ACB_RUSTFS_BUCKET", "acb-artifacts"),
		RustFSRegion:       getEnv("ACB_RUSTFS_REGION", getEnv("RUSTFS_REGION", "us-east-1")),
		RustFSAccessKey:    getEnv("RUSTFS_ACCESS_KEY_ID", ""),
		RustFSSecretKey:    getEnv("RUSTFS_SECRET_ACCESS_KEY", ""),
		LogLevel:           getEnv("ACB_LOG_LEVEL", "info"),
		ArtifactTTLDays:    getEnvInt("ACB_ARTIFACT_TTL_DAYS", 30),
		MaxUploadSizeMB:    getEnvInt("ACB_MAX_UPLOAD_SIZE_MB", 32),
		PendingTimeoutMin:      getEnvInt("ACB_PENDING_TIMEOUT_MIN", 15),
		PendingTimeoutCheckSec: getEnvInt("ACB_PENDING_TIMEOUT_CHECK_SEC", 60),
		TaskTimeoutMin:         getEnvInt("ACB_TASK_TIMEOUT_MIN", 30),
		TaskTimeoutCheckSec:    getEnvInt("ACB_TASK_TIMEOUT_CHECK_SEC", 60),
		AgentStaleMin:          getEnvInt("ACB_AGENT_STALE_MIN", 10),
		AgentStaleCheckSec:     getEnvInt("ACB_AGENT_STALE_CHECK_SEC", 60),
		AllowedSkills:          getEnvList("ACB_ALLOWED_SKILLS", "coding,review,testing,architecture,devops,security,infra,debugging,documentation,osint,hacking,forensics,go,python,orchestration,dispatch,management"),
		AllowedTags:            getEnvList("ACB_ALLOWED_TAGS", ""),
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

func getEnvList(key, fallback string) []string {
	v := os.Getenv(key)
	if v == "" {
		v = fallback
	}
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// IsValidSkill checks if a skill is in the allowed list.
// If the allowed list is empty (not configured), all skills are valid.
// Nil receiver is treated as "no restrictions" — all skills are valid.
func (c *Config) IsValidSkill(skill string) bool {
	if c == nil || len(c.AllowedSkills) == 0 {
		return true
	}
	for _, s := range c.AllowedSkills {
		if s == skill {
			return true
		}
	}
	return false
}

// ValidateSkills returns the list of invalid skills from the input.
func (c *Config) ValidateSkills(skills []string) []string {
	var invalid []string
	for _, s := range skills {
		if !c.IsValidSkill(s) {
			invalid = append(invalid, s)
		}
	}
	return invalid
}

// IsValidTag checks if a tag is in the allowed list.
// If the allowed list is empty (not configured), all tags are valid.
// Nil receiver is treated as "no restrictions" — all tags are valid.
func (c *Config) IsValidTag(tag string) bool {
	if c == nil || len(c.AllowedTags) == 0 {
		return true
	}
	for _, t := range c.AllowedTags {
		if t == tag {
			return true
		}
	}
	return false
}

// ValidateTags returns the list of invalid tags from the input.
func (c *Config) ValidateTags(tags []string) []string {
	var invalid []string
	for _, t := range tags {
		if !c.IsValidTag(t) {
			invalid = append(invalid, t)
		}
	}
	return invalid
}