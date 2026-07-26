package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	JWTSecret       string
	JWTIssuer       string
	ListenAddr      string
	GRPCAddr        string
	PostgresDSN     string
	RedisAddr       string
	RedisDB         int
	RedisPass       string
	KafkaBrokers    []string
	KafkaTopic      string
	TradingPairs    []string
	AlchemyAPIKey   string
	AlchemyEthURL   string
	AlchemyPolygonURL string
	LogLevel        string
	LogFormat       string
	Environment     string
	DevMode         bool

	// Security
	CORSAllowOrigins []string
	CORSAllowCreds   bool
	RateLimitPerSec  int
	RateLimitBurst   int
	AccountLockoutThreshold int
	AccountLockoutDurationMin int

	// Feature flags
	EnableAPIKeyAuth bool
	EnableRedisRateLimit bool
}

func Load() *Config {
	key := getEnv("ALCHEMY_API_KEY", "")
	env := getEnv("ENVIRONMENT", "development")
	cfg := &Config{
		JWTSecret:  getEnv("JWT_SECRET", "nexa-dev-secret"),
		JWTIssuer:  getEnv("JWT_ISSUER", "nexa-exchange"),
		ListenAddr: getEnv("LISTEN_ADDR", ":8080"),
		GRPCAddr:   getEnv("GRPC_ADDR", ":50051"),
		PostgresDSN: getEnv("POSTGRES_DSN", "postgres://nexa:nexa_dev@localhost:5432/nexa?sslmode=disable"),
		RedisAddr:   getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPass:   getEnv("REDIS_PASSWORD", ""),
		RedisDB:     getEnvAsInt("REDIS_DB", 0),
		KafkaBrokers: strings.Split(getEnv("KAFKA_BROKERS", "localhost:9092"), ","),
		KafkaTopic:   getEnv("KAFKA_TOPIC", "nexa-exchange"),
		AlchemyAPIKey:     key,
		AlchemyEthURL:     getEnv("ALCHEMY_ETH_URL", "https://eth-mainnet.g.alchemy.com/v2/"+key),
		AlchemyPolygonURL: getEnv("ALCHEMY_POLYGON_URL", "https://polygon-mainnet.g.alchemy.com/v2/"+key),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		LogFormat:   getEnv("LOG_FORMAT", "text"),
		Environment: env,
		TradingPairs: strings.Split(getEnv("TRADING_PAIRS", "BTC/USDT,ETH/USDT,SOL/USDT,BNB/USDT,ADA/USDT"), ","),
		DevMode: env == "development",

		CORSAllowOrigins: strings.Split(getEnv("CORS_ALLOW_ORIGINS", "*"), ","),
		CORSAllowCreds:   getEnv("CORS_ALLOW_CREDENTIALS", "false") == "true",
		RateLimitPerSec:  getEnvAsInt("RATE_LIMIT_PER_SEC", 100),
		RateLimitBurst:   getEnvAsInt("RATE_LIMIT_BURST", 200),
		AccountLockoutThreshold: getEnvAsInt("ACCOUNT_LOCKOUT_THRESHOLD", 5),
		AccountLockoutDurationMin: getEnvAsInt("ACCOUNT_LOCKOUT_DURATION_MIN", 15),

		EnableAPIKeyAuth:    getEnv("ENABLE_API_KEY_AUTH", "true") == "true",
		EnableRedisRateLimit: getEnv("ENABLE_REDIS_RATE_LIMIT", "false") == "true",
	}
	if cfg.JWTSecret == "nexa-dev-secret" && !cfg.DevMode {
		fmt.Println("[WARN] JWT_SECRET is set to default in non-development environment!")
	}
	if env == "production" {
		// In production, refuse to start with wildcard CORS unless explicitly
		// overridden.
		for _, o := range cfg.CORSAllowOrigins {
			if o == "*" {
				fmt.Println("[WARN] CORS_ALLOW_ORIGINS=* in production; refusing to enable credentials")
				cfg.CORSAllowCreds = false
			}
		}
	}
	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvAsInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvAsBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "true" || v == "1" || v == "yes"
	}
	return fallback
}
