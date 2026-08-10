package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	JWTSecret         string
	JWTIssuer         string
	ListenAddr        string
	GRPCAddr          string
	PostgresDSN       string
	RedisAddr         string
	RedisDB           int
	RedisPass         string
	KafkaBrokers      []string
	KafkaTopic        string
	TradingPairs      []string
	AlchemyAPIKey     string
	AlchemyEthURL     string
	AlchemyPolygonURL string
	LogLevel          string
	LogFormat         string
	Environment       string
	DevMode           bool

	// Security
	CORSAllowOrigins          []string
	CORSAllowCreds            bool
	RateLimitPerSec           int
	RateLimitBurst            int
	AccountLockoutThreshold   int
	AccountLockoutDurationMin int

	// HTTP / WebSocket hardening
	MaxRequestBodyBytes int
	WSMaxConnections    int
	WSConnRatePerMin    int

	// TLS / SSL
	TLSCertFile string
	TLSKeyFile  string
	TLSAutoCert bool

	// Feature flags
	EnableAPIKeyAuth     bool
	EnableRedisRateLimit bool

	// Market data sources (Binance real market data primary, AMM fallback)
	BinanceRESTURLs       []string
	BinanceWSURL          string
	BinancePollInterval   time.Duration
	MarketDataStaleness   time.Duration
	EnableMarketSimulator bool
}

func Load() *Config {
	key := getEnv("ALCHEMY_API_KEY", "")
	env := getEnv("ENVIRONMENT", "development")
	// LOG_FORMAT defaults to text in development (human friendly) and json
	// everywhere else (machine parseable by log aggregators).
	logFormatDefault := "json"
	if env == "development" {
		logFormatDefault = "text"
	}
	cfg := &Config{
		JWTSecret:  getEnv("JWT_SECRET", "nexa-dev-secret"),
		JWTIssuer:  getEnv("JWT_ISSUER", "nexa-exchange"),
		ListenAddr: getEnv("LISTEN_ADDR", ":8080"),
		GRPCAddr:   getEnv("GRPC_ADDR", ":50051"),
		// Default matches deploy/docker/docker-compose.yml (user=nexa, password=nexa_dev,
		// db=nexa, host port 5433). Use 127.0.0.1 instead of localhost so the Go
		// resolver picks IPv4 — on Windows, "localhost" often resolves to IPv6 ::1,
		// which WSL's port relay can shadow and route to the wrong database.
		//
		// Supabase (managed PostgreSQL): set POSTGRES_DSN to the Session pooler
		// / Direct connection string (port 5432) with sslmode=require — Supabase
		// enforces TLS. Do NOT use the Transaction pooler (port 6543): the store
		// layer uses lib/pq prepared statements, which fail in PgBouncer
		// transaction-pooling mode with `unnamed prepared statement does not
		// exist`. See .env.example.
		PostgresDSN:       getEnv("POSTGRES_DSN", "postgres://nexa:nexa_dev@127.0.0.1:5433/nexa?sslmode=disable"),
		RedisAddr:         getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPass:         getEnv("REDIS_PASSWORD", ""),
		RedisDB:           getEnvAsInt("REDIS_DB", 0),
		KafkaBrokers:      strings.Split(getEnv("KAFKA_BROKERS", "localhost:9092"), ","),
		KafkaTopic:        getEnv("KAFKA_TOPIC", "nexa-exchange"),
		AlchemyAPIKey:     key,
		AlchemyEthURL:     getEnv("ALCHEMY_ETH_URL", "https://eth-mainnet.g.alchemy.com/v2/"+key),
		AlchemyPolygonURL: getEnv("ALCHEMY_POLYGON_URL", "https://polygon-mainnet.g.alchemy.com/v2/"+key),
		LogLevel:          getEnv("LOG_LEVEL", "info"),
		LogFormat:         getEnv("LOG_FORMAT", logFormatDefault),
		Environment:       env,
		TradingPairs:      strings.Split(getEnv("TRADING_PAIRS", "BTC/USDT,ETH/USDT,SOL/USDT,BNB/USDT,ADA/USDT"), ","),
		DevMode:           env == "development",

		CORSAllowOrigins:          strings.Split(getEnv("CORS_ALLOW_ORIGINS", "*"), ","),
		CORSAllowCreds:            getEnv("CORS_ALLOW_CREDENTIALS", "false") == "true",
		RateLimitPerSec:           getEnvAsInt("RATE_LIMIT_PER_SEC", 100),
		RateLimitBurst:            getEnvAsInt("RATE_LIMIT_BURST", 200),
		AccountLockoutThreshold:   getEnvAsInt("ACCOUNT_LOCKOUT_THRESHOLD", 5),
		AccountLockoutDurationMin: getEnvAsInt("ACCOUNT_LOCKOUT_DURATION_MIN", 15),

		MaxRequestBodyBytes: getEnvAsInt("MAX_REQUEST_BODY_BYTES", 1<<20),
		WSMaxConnections:    getEnvAsInt("WS_MAX_CONNECTIONS", 10000),
		WSConnRatePerMin:    getEnvAsInt("WS_CONNECT_RATE_PER_MIN", 30),

		TLSCertFile: getEnv("TLS_CERT_FILE", ""),
		TLSKeyFile:  getEnv("TLS_KEY_FILE", ""),
		TLSAutoCert: getEnv("TLS_AUTO_CERT", "false") == "true",

		EnableAPIKeyAuth:     getEnv("ENABLE_API_KEY_AUTH", "true") == "true",
		EnableRedisRateLimit: getEnv("ENABLE_REDIS_RATE_LIMIT", "false") == "true",

		// Market data: Binance public mirrors first (api.binance.com is
		// geo-blocked in some regions), the canonical host second and the
		// third-party mirror as the last-resort degraded REST source.
		BinanceRESTURLs: splitAndTrim(getEnv("BINANCE_REST_URLS", strings.Join(marketDefaultRESTURLs, ","))),
		BinanceWSURL:    getEnv("BINANCE_WS_URL", "wss://data-stream.binance.vision:443/stream"),
		BinancePollInterval:   getEnvAsDuration("BINANCE_POLL_INTERVAL", 5*time.Second),
		MarketDataStaleness:   getEnvAsDuration("MARKET_DATA_STALENESS", 10*time.Second),
		EnableMarketSimulator: getEnvAsBool("ENABLE_MARKET_SIMULATOR", false),
	}
	// JWT secret enforcement: outside development a weak or default secret
	// is a fatal misconfiguration — refuse to start instead of warning.
	if !cfg.DevMode {
		if cfg.JWTSecret == "nexa-dev-secret" {
			slog.Error("JWT_SECRET is the built-in default; set a strong JWT_SECRET (>=32 bytes) in non-development environments")
			os.Exit(1)
		}
		if len(cfg.JWTSecret) < 32 {
			slog.Error("JWT_SECRET is shorter than 32 bytes; refusing to start in non-development environment")
			os.Exit(1)
		}
	} else if cfg.JWTSecret == "nexa-dev-secret" || len(cfg.JWTSecret) < 32 {
		slog.Warn("JWT_SECRET is the default or shorter than 32 bytes; acceptable only in development")
	}
	// CORS tightening: outside development the wildcard origin is forbidden.
	// The API must be given an explicit allow-list (CORS_ALLOW_ORIGINS).
	if !cfg.DevMode {
		for _, o := range cfg.CORSAllowOrigins {
			if strings.TrimSpace(o) == "*" {
				slog.Error("CORS_ALLOW_ORIGINS=* is forbidden outside development; configure explicit allowed origins")
				os.Exit(1)
			}
		}
	} else {
		slog.Warn("development environment: wildcard/mixed CORS origins are permitted only here")
	}
	return cfg
}

func (c *Config) HasTLS() bool {
	return (c.TLSCertFile != "" && c.TLSKeyFile != "") || c.TLSAutoCert
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

func getEnvAsDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return fallback
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// marketDefaultRESTURLs mirrors market.DefaultBinanceMirrors without
// importing the market package (config must stay dependency-free).
var marketDefaultRESTURLs = []string{
	"https://data-api.binance.vision",
	"https://api.binance.com",
	"https://www.usnbweb.red",
}
