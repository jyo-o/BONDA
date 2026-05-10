package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	// Core
	DatabaseURL      string
	EthRPCURL        string
	DataAPIBaseURL   string
	EigenDADirectory string

	// Worker toggles
	CollectorEnabled        bool
	RelayVerifierEnabled    bool
	OperatorVerifierEnabled bool
	ReverifierEnabled       bool
	WriteProberEnabled      bool
	StakeIndexerEnabled     bool
	EjectionIndexerEnabled  bool
	KZGVerifyEnabled        bool
	OperatorStatusEnabled   bool

	// Tuning
	CollectorPollInterval  time.Duration
	RelayVerifierParallel  int
	ReverifierInterval     time.Duration
	WriteProberInterval    time.Duration
	StakeIndexerInterval   time.Duration
	EjectionPollInterval     time.Duration
	OperatorStatusInterval   time.Duration

	// Write prober (optional)
	EigenDAProxyURL     string
	DisperserPrivateKey string
	DisperserAccountID  string

	// API
	APIListenAddr     string
	MetricsListenAddr string
}

func Load() *Config {
	return &Config{
		DatabaseURL:      getEnv("TIMESCALEDB_URL", "postgres://bonda:bonda@localhost:5432/bonda?sslmode=disable"),
		EthRPCURL:        getEnv("ETH_RPC_URL", ""),
		DataAPIBaseURL:   getEnv("DATAAPI_BASE_URL", "https://dataapi.eigenda.xyz/api/v2"),
		EigenDADirectory: getEnv("EIGENDA_DIRECTORY", "0x64AB2e9A86FA2E183CB6f01B2D4050c1c2dFAad4"),

		CollectorEnabled:        getBoolEnv("COLLECTOR_ENABLED", true),
		RelayVerifierEnabled:    getBoolEnv("RELAY_VERIFIER_ENABLED", true),
		OperatorVerifierEnabled: getBoolEnv("OPERATOR_VERIFIER_ENABLED", true),
		ReverifierEnabled:       getBoolEnv("REVERIFIER_ENABLED", true),
		WriteProberEnabled:      getBoolEnv("WRITE_PROBER_ENABLED", false),
		StakeIndexerEnabled:     getBoolEnv("STAKE_INDEXER_ENABLED", true),
		EjectionIndexerEnabled:  getBoolEnv("EJECTION_INDEXER_ENABLED", true),
		KZGVerifyEnabled:        getBoolEnv("KZG_VERIFY_ENABLED", true),
		OperatorStatusEnabled:   getBoolEnv("OPERATOR_STATUS_ENABLED", false),

		CollectorPollInterval:  getDurationEnv("COLLECTOR_POLL_INTERVAL", 3*time.Second),
		RelayVerifierParallel:  getIntEnv("RELAY_VERIFIER_PARALLEL", 10),
		ReverifierInterval:     getDurationEnv("REVERIFIER_INTERVAL", 5*time.Minute),
		WriteProberInterval:    getDurationEnv("WRITE_PROBER_INTERVAL", 5*time.Minute),
		StakeIndexerInterval:   getDurationEnv("STAKE_INDEXER_INTERVAL", 1*time.Hour),
		EjectionPollInterval:   getDurationEnv("EJECTION_POLL_INTERVAL", 12*time.Second),
		OperatorStatusInterval: getDurationEnv("OPERATOR_STATUS_INTERVAL", 6*time.Hour),

		EigenDAProxyURL:     getEnv("EIGENDA_PROXY_URL", "http://eigenda-proxy:3100"),
		DisperserPrivateKey: getEnv("DISPERSER_PRIVATE_KEY", ""),
		DisperserAccountID: getEnv("DISPERSER_ACCOUNT_ID", ""),

		APIListenAddr:     getEnv("API_LISTEN_ADDR", ":8080"),
		MetricsListenAddr: getEnv("METRICS_LISTEN_ADDR", ":9090"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getBoolEnv(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v == "true" || v == "1"
}

func getIntEnv(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
