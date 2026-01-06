package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the application
type Config struct {
	Gateway          GatewayConfig          `json:"gateway" yaml:"gateway"`
	Chat             ChatConfig             `json:"chat" yaml:"chat"`
	Presence         PresenceConfig         `json:"presence" yaml:"presence"`
	Call             CallConfig             `json:"call" yaml:"call"`
	Database         DatabaseConfig         `json:"database" yaml:"database"`
	Redis            RedisConfig            `json:"redis" yaml:"redis"`
	ServiceDiscovery ServiceDiscoveryConfig `json:"serviceDiscovery" yaml:"serviceDiscovery"`
	Vault            VaultConfig            `json:"vault" yaml:"vault"`
}

// GatewayConfig holds API Gateway configuration
type GatewayConfig struct {
	Port         string        `json:"port" yaml:"port"`
	JWTSecret    string        `json:"jwtSecret" yaml:"jwtSecret"`
	RateLimit    int           `json:"rateLimit" yaml:"rateLimit"`
	ReadTimeout  time.Duration `json:"readTimeout" yaml:"readTimeout"`
	WriteTimeout time.Duration `json:"writeTimeout" yaml:"writeTimeout"`
}

// ChatConfig holds Chat service configuration
type ChatConfig struct {
	Port string `json:"port" yaml:"port"`
}

// PresenceConfig holds Presence Service Configuration
type PresenceConfig struct {
	Port      string        `json:"port" yaml:"port"`
	TTL       time.Duration `json:"ttl" yaml:"ttl"`
	BatchSize int           `json:"batchSize" yaml:"batchSize"`
}

// CallConfig holds call service configuration
type CallConfig struct {
	Port string `json:"port" yaml:"port"`
}

// DatabaseConfig holds PostgreSQL configuration
type DatabaseConfig struct {
	Host            string        `json:"host" yaml:"host"`
	Port            string        `json:"port" yaml:"port"`
	User            string        `json:"user" yaml:"user"`
	Password        string        `json:"password" yaml:"password"`
	Database        string        `json:"database" yaml:"database"`
	MaxConnections  int           `json:"maxConnections" yaml:"maxConnections"`
	MaxIdleConns    int           `json:"maxIdleConns" yaml:"maxIdleConns"`
	ConnMaxLifetime time.Duration `json:"connMaxLifetime" yaml:"connMaxLifetime"`
	SSLMode         string        `json:"sslMode" yaml:"sslMode"`
}

// RedisConfig holds Redis Configuration
type RedisConfig struct {
	Addresses    []string `json:"addresses" yaml:"addresses"`
	Password     string   `json:"password" yaml:"password"`
	DB           int      `json:"db" yaml:"db"`
	PoolSize     int      `json:"poolSize" yaml:"poolSize"`
	MinIdleConns int      `json:"minIdleConns" yaml:"minIdleConns"`
}

// ServiceDiscoveryConfig holds service discovery configuration
type ServiceDiscoveryConfig struct {
	Type     string        `json:"type" yaml:"type"`
	Address  string        `json:"address" yaml:"address"`
	Interval time.Duration `json:"interval" yaml:"interval"`
}

// VaultConfig holds secret vault configuration
type VaultConfig struct {
	Enabled    bool          `json:"enabled" yaml:"enabled"`
	Address    string        `json:"address" yaml:"address"`
	Token      string        `json:"token" yaml:"token"`
	SecretPath string        `json:"secretPath" yaml:"secretPath"`
	K8sAuth    K8sAuthConfig `json:"k8sAuth" yaml:"k8sAuth"`
}

// K8sAuthConfig holds Kubernetes authentication configuration for Vault
type K8sAuthConfig struct {
	Enabled        bool   `json:"enabled" yaml:"enabled"`
	Role           string `json:"role" yaml:"role"`
	ServiceAccount string `json:"serviceAccount" yaml:"serviceAccount"`
}

// SecretProvider interface for different secret backends
type SecretProvider interface {
	GetSecret(path string) (map[string]interface{}, error)
}

// ConfigSource represents the source of configuration
type ConfigSource string

const (
	ConfigSourceFile       ConfigSource = "file"
	ConfigSourceVault      ConfigSource = "vault"
	ConfigSourceKubernetes ConfigSource = "kubernetes"
	ConfigSourceHelm       ConfigSource = "helm"
)

// LoadOptions holds options for loading configuration
type LoadOptions struct {
	Source      ConfigSource
	ConfigPath  string
	VaultConfig *VaultConfig
}

// Load loads configurations from environment variables with defaults
func Load() (*Config, error) {
	// Determine configuration source from environment
	source := getConfigSource()

	switch source {
	case ConfigSourceFile:
		return loadFromFile()
	case ConfigSourceVault:
		return loadFromVault()
	case ConfigSourceKubernetes:
		return loadFromKubernetes()
	case ConfigSourceHelm:
		return loadFromHelm()
	default:
		return loadDefaults()
	}
}

// LoadWithOptions loads configuration with specific options
func LoadWithOptions(opts LoadOptions) (*Config, error) {
	switch opts.Source {
	case ConfigSourceFile:
		return loadFromFileWithPath(opts.ConfigPath)
	case ConfigSourceVault:
		return loadFromVaultWithConfig(opts.VaultConfig)
	case ConfigSourceKubernetes:
		return loadFromKubernetes()
	case ConfigSourceHelm:
		return loadFromHelm()
	default:
		return loadDefaults()
	}
}

// getConfigSource determines the configuration source from environment
func getConfigSource() ConfigSource {
	if source := os.Getenv("CONFIG_SOURCE"); source != "" {
		return ConfigSource(source)
	}

	if os.Getenv("VAULT_ADDR") != "" {
		return ConfigSourceVault
	}

	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return ConfigSourceKubernetes
	}

	if os.Getenv("HELM_RELEASE_NAME") != "" {
		return ConfigSourceHelm
	}

	return ConfigSourceFile
}

// loadDefaults loads default configuration values
func loadDefaults() (*Config, error) {
	cfg := &Config{
		Gateway: GatewayConfig{
			Port:         ":8080",
			JWTSecret:    "change-me-in-production",
			RateLimit:    1000,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
		},
		Chat: ChatConfig{
			Port: ":8081",
		},
		Presence: PresenceConfig{
			Port:      ":8082",
			TTL:       time.Duration(30) * time.Second,
			BatchSize: 100,
		},
		Call: CallConfig{
			Port: ":8083",
		},
		Database: DatabaseConfig{
			Host:            "localhost",
			Port:            "5432",
			User:            "postgres",
			Password:        "postgres",
			Database:        "chatplatform",
			MaxConnections:  100,
			MaxIdleConns:    10,
			ConnMaxLifetime: time.Duration(1) * time.Hour,
			SSLMode:         "disable",
		},
		Redis: RedisConfig{
			Addresses:    []string{"localhost:6379"},
			Password:     "",
			DB:           0,
			PoolSize:     100,
			MinIdleConns: 10,
		},
		ServiceDiscovery: ServiceDiscoveryConfig{
			Type:     "memory",
			Address:  "localhost:8500",
			Interval: time.Duration(10) * time.Second,
		},
		Vault: VaultConfig{
			Enabled: false,
		},
	}

	return cfg, nil
}

// loadFromFile loads configuration from a JSON or YAML file
func loadFromFile() (*Config, error) {
	configPath := os.Getenv("CONFIG_FILE_PATH")
	if configPath == "" {
		configPath = "/etc/chat-platform/config.json"
	}

	return loadFromFileWithPath(configPath)
}

// loadFromFileWithPath loads configuration from a specific file path
func loadFromFileWithPath(configPath string) (*Config, error) {
	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Fall back to defaults if config file doesn't exist
		return loadDefaults()
	}

	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config

	// Try JSON first, then YAML
	if err := json.Unmarshal(data, &cfg); err != nil {
		// If JSON fails, try YAML (would need yaml package)
		return nil, fmt.Errorf("failed to parse config file as JSON: %w", err)
	}
	return &cfg, nil
}

// loadFromVault loads configuration from HashiCorp vault
func loadFromVault() (*Config, error) {
	vaultConfig := &VaultConfig{
		Enabled:    true,
		Address:    os.Getenv("VAULT_ADDR"),
		Token:      os.Getenv("VAULT_TOKEN"),
		SecretPath: getEnvWithDefault("VAULT_SECRET_PATH", "secret/chat-platform"),
	}
	return loadFromVaultWithConfig(vaultConfig)
}

// loadFromVaultWithConfig loads configuration from Vault with specific config
func loadFromVaultWithConfig(vaultConfig *VaultConfig) (*Config, error) {
	if !vaultConfig.Enabled {
		return loadDefaults()
	}

	// This is a placeholder for Vault integration
	// In a real implement, we would use the Vault API Client
	// For now, return defaults with a note that Vault integration is needed
	cfg, err := loadDefaults()
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// loadFromKubernetes loads configuration from Kubernetes secrets and configmaps
func loadFromKubernetes() (*Config, error) {
	cfg, err := loadDefaults()
	if err != nil {
		return nil, err
	}

	// Load from kubernetes secrets mounted as files
	secretsPath := "/var/secrets"

	// Database secrets
	if dbPassword := readK8sSecret(secretsPath + "/database/password"); dbPassword != "" {
		cfg.Database.Password = dbPassword
	}

	if dbUser := readK8sSecret(secretsPath + "/database/username"); dbUser != "" {
		cfg.Database.User = dbUser
	}

	// Redis secrets
	if redisPassword := readK8sSecret(secretsPath + "/redis/password"); redisPassword != "" {
		cfg.Redis.Password = redisPassword
	}

	// JWT secrets
	if jwtSecret := readK8sSecret(secretsPath + "/jwt/secret"); jwtSecret != "" {
		cfg.Gateway.JWTSecret = jwtSecret
	}

	// Load from ConfigMap environment variables (non-sensitive config)
	if dbHost := os.Getenv("DATABASE_HOST"); dbHost != "" {
		cfg.Database.Host = dbHost
	}

	if dbPort := os.Getenv("DATABASE_PORT"); dbPort != "" {
		cfg.Database.Port = dbPort
	}

	if redisAddress := os.Getenv("REDIS_ADDRESSES"); redisAddress != "" {
		cfg.Redis.Addresses = strings.Split(redisAddress, ",")
	}

	return cfg, nil
}

// loadFromHelm loads Configuration from Helm values
func loadFromHelm() (*Config, error) {
	cfg, err := loadDefaults()
	if err != nil {
		return nil, err
	}

	// Helm typically injects configuration via environment variables or mounted files
	// Load from Helm-injected environment variables

	// Gateway configuration
	if port := os.Getenv("HELM_GATEWAY_PORT"); port != "" {
		cfg.Gateway.Port = port
	}
	if rateLimit := os.Getenv("HELM_GATEWAY_RATE_LIMIT"); rateLimit != "" {
		if val, err := strconv.Atoi(rateLimit); err == nil {
			cfg.Gateway.RateLimit = val
		}
	}

	// Database configuration
	if host := os.Getenv("HELM_DATABASE_HOST"); host != "" {
		cfg.Database.Host = host
	}
	if port := os.Getenv("HELM_DATABASE_PORT"); port != "" {
		cfg.Database.Port = port
	}
	if database := os.Getenv("HELM_DATABASE_NAME"); database != "" {
		cfg.Database.Database = database
	}

	// Redis configuration
	if addresses := os.Getenv("HELM_REDIS_ADDRESSES"); addresses != "" {
		cfg.Redis.Addresses = strings.Split(addresses, ",")
	}

	// Load secrets from Helm secret mounts
	helmSecretsPath := "/etc/helm-secrets"

	if dbPassword := readK8sSecret(helmSecretsPath + "/database-password"); dbPassword != "" {
		cfg.Database.Password = dbPassword
	}
	if jwtSecret := readK8sSecret(helmSecretsPath + "/jwt-secret"); jwtSecret != "" {
		cfg.Gateway.JWTSecret = jwtSecret
	}
	if redisPassword := readK8sSecret(helmSecretsPath + "/redis-password"); redisPassword != "" {
		cfg.Redis.Password = redisPassword
	}

	return cfg, nil
}

// readK8sSecret reads a secret from a mounted file
func readK8sSecret(path string) string {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// getEnvWithDefault gets environment variable with default value
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// DatabaseURL returns the PostgreSQL connection string
func (c *DatabaseConfig) DatabaseURL() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		c.User, c.Password, c.Host, c.Port, c.Database, c.SSLMode)
}

// GetReadTimeout returns the parsed read timeout duration
func (c *GatewayConfig) GetReadTimeout() time.Duration {
	if c.ReadTimeout > 0 {
		return c.ReadTimeout
	}
	return 15 * time.Second // default
}

// GetWriteTimeout returns the parsed write timeout duration
func (c *GatewayConfig) GetWriteTimeout() time.Duration {
	if c.WriteTimeout > 0 {
		return c.WriteTimeout
	}
	return 15 * time.Second // default
}

// GetTTL returns the parsed TTL duration
func (c *PresenceConfig) GetTTL() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return 30 * time.Second // default
}

// GetConnMaxLifetime returns the parsed connection max lifetime duration
func (c *DatabaseConfig) GetConnMaxLifetime() time.Duration {
	if c.ConnMaxLifetime > 0 {
		return c.ConnMaxLifetime
	}
	return time.Hour // default
}

// GetInterval returns the parsed interval duration
func (c *ServiceDiscoveryConfig) GetInterval() time.Duration {
	if c.Interval > 0 {
		return c.Interval
	}
	return 10 * time.Second // default
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Gateway.JWTSecret == "change-me-in-production" {
		return fmt.Errorf("JWT secret must be changed in production")
	}

	if c.Database.Password == "" {
		return fmt.Errorf("database password is required")
	}

	if len(c.Redis.Addresses) == 0 {
		return fmt.Errorf("at least one Redis address is required")
	}

	return nil
}
