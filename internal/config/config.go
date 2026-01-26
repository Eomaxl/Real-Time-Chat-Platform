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
	SFU              SFUConfig              `json:"sfu" yaml:"sfu"`
	Database         DatabaseConfig         `json:"database" yaml:"database"`
	Redis            RedisConfig            `json:"redis" yaml:"redis"`
	ServiceDiscovery ServiceDiscoveryConfig `json:"serviceDiscovery" yaml:"serviceDiscovery"`
	Vault            VaultConfig            `json:"vault" yaml:"vault"`
	MultiRegion      MultiRegionConfig      `json:"multiRegion" yaml:"multiRegion"`
	Kafka            KafkaConfig            `json:"kafka" yaml:"kafka"`
}

// GatewayConfig holds API Gateway configuration
type GatewayConfig struct {
	Port         string `json:"port" yaml:"port"`
	JWTSecret    string `json:"jwtSecret" yaml:"jwtSecret"`
	RateLimit    int    `json:"rateLimit" yaml:"rateLimit"`
	ReadTimeout  string `json:"readTimeout" yaml:"readTimeout"`
	WriteTimeout string `json:"writeTimeout" yaml:"writeTimeout"`
}

// ChatConfig holds Chat Service configuration
type ChatConfig struct {
	Port string `json:"port" yaml:"port"`
}

// PresenceConfig holds Presence Service configuration
type PresenceConfig struct {
	Port      string `json:"port" yaml:"port"`
	TTL       string `json:"ttl" yaml:"ttl"`
	BatchSize int    `json:"batchSize" yaml:"batchSize"`
}

// CallConfig holds Call Service configuration
type CallConfig struct {
	Port string `json:"port" yaml:"port"`
}

// SFUConfig holds SFU Service configuration
type SFUConfig struct {
	Port string `json:"port" yaml:"port"`
}

// DatabaseConfig holds PostgreSQL configuration
type DatabaseConfig struct {
	Host            string          `json:"host" yaml:"host"`
	Port            string          `json:"port" yaml:"port"`
	User            string          `json:"user" yaml:"user"`
	Password        string          `json:"password" yaml:"password"`
	Database        string          `json:"database" yaml:"database"`
	MaxConnections  int             `json:"maxConnections" yaml:"maxConnections"`
	MaxIdleConns    int             `json:"maxIdleConns" yaml:"maxIdleConns"`
	ConnMaxLifetime string          `json:"connMaxLifetime" yaml:"connMaxLifetime"`
	SSLMode         string          `json:"sslMode" yaml:"sslMode"`
	Shards          []ShardConfig   `json:"shards" yaml:"shards"`
	ReadReplicas    []ReplicaConfig `json:"readReplicas" yaml:"readReplicas"`
}

// ShardConfig holds configuration for a database shard
type ShardConfig struct {
	Name     string `json:"name" yaml:"name"`
	Host     string `json:"host" yaml:"host"`
	Port     string `json:"port" yaml:"port"`
	Database string `json:"database" yaml:"database"`
	Weight   int    `json:"weight" yaml:"weight"` // For weighted consistent hashing
}

// ReplicaConfig holds configuration for a read replica
type ReplicaConfig struct {
	Name     string `json:"name" yaml:"name"`
	Host     string `json:"host" yaml:"host"`
	Port     string `json:"port" yaml:"port"`
	Database string `json:"database" yaml:"database"`
	ShardID  int    `json:"shardId" yaml:"shardId"` // Which shard this replica belongs to
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Addresses    []string `json:"addresses" yaml:"addresses"`
	Password     string   `json:"password" yaml:"password"`
	DB           int      `json:"db" yaml:"db"`
	PoolSize     int      `json:"poolSize" yaml:"poolSize"`
	MinIdleConns int      `json:"minIdleConns" yaml:"minIdleConns"`
}

// ServiceDiscoveryConfig holds service discovery configuration
type ServiceDiscoveryConfig struct {
	Type     string `json:"type" yaml:"type"` // "memory" for development, "consul" for production
	Address  string `json:"address" yaml:"address"`
	Interval string `json:"interval" yaml:"interval"`
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

// MultiRegionConfig holds multi-region configuration
type MultiRegionConfig struct {
	Enabled             bool           `json:"enabled" yaml:"enabled"`
	CurrentRegion       string         `json:"currentRegion" yaml:"currentRegion"`
	Regions             []RegionConfig `json:"regions" yaml:"regions"`
	ReplicationMode     string         `json:"replicationMode" yaml:"replicationMode"` // "async", "sync", "semi-sync"
	FailoverEnabled     bool           `json:"failoverEnabled" yaml:"failoverEnabled"`
	FailoverTimeout     string         `json:"failoverTimeout" yaml:"failoverTimeout"`
	HealthCheckInterval string         `json:"healthCheckInterval" yaml:"healthCheckInterval"`
	ConflictResolution  string         `json:"conflictResolution" yaml:"conflictResolution"` // "last-write-wins", "vector-clock"
}

// RegionConfig holds configuration for a specific region
type RegionConfig struct {
	Name         string   `json:"name" yaml:"name"`
	Code         string   `json:"code" yaml:"code"` // e.g., "us-east", "eu-west"
	IsPrimary    bool     `json:"isPrimary" yaml:"isPrimary"`
	DatabaseURLs []string `json:"databaseUrls" yaml:"databaseUrls"`
	RedisURLs    []string `json:"redisUrls" yaml:"redisUrls"`
	KafkaURLs    []string `json:"kafkaUrls" yaml:"kafkaUrls"`
	Priority     int      `json:"priority" yaml:"priority"` // For failover ordering
	HealthURL    string   `json:"healthUrl" yaml:"healthUrl"`
}

// KafkaConfig holds Apache Kafka configuration for cross-region event streaming
type KafkaConfig struct {
	Enabled       bool     `json:"enabled" yaml:"enabled"`
	Brokers       []string `json:"brokers" yaml:"brokers"`
	Topic         string   `json:"topic" yaml:"topic"`
	ConsumerGroup string   `json:"consumerGroup" yaml:"consumerGroup"`
	Compression   string   `json:"compression" yaml:"compression"` // "none", "gzip", "snappy", "lz4", "zstd"
	BatchSize     int      `json:"batchSize" yaml:"batchSize"`
	FlushInterval string   `json:"flushInterval" yaml:"flushInterval"`
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

// Load loads configuration from the specified source
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

	// Auto-detect based on environment
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
			ReadTimeout:  "15s",
			WriteTimeout: "15s",
		},
		Chat: ChatConfig{
			Port: ":8081",
		},
		Presence: PresenceConfig{
			Port:      ":8082",
			TTL:       "30s",
			BatchSize: 100,
		},
		Call: CallConfig{
			Port: ":8083",
		},
		SFU: SFUConfig{
			Port: ":8084",
		},
		Database: DatabaseConfig{
			Host:            "localhost",
			Port:            "5432",
			User:            "postgres",
			Password:        "postgres",
			Database:        "chatplatform",
			MaxConnections:  100,
			MaxIdleConns:    10,
			ConnMaxLifetime: "1h",
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
			Interval: "10s",
		},
		Vault: VaultConfig{
			Enabled: false,
		},
		MultiRegion: MultiRegionConfig{
			Enabled:             false,
			CurrentRegion:       "us-east",
			ReplicationMode:     "async",
			FailoverEnabled:     false,
			FailoverTimeout:     "30s",
			HealthCheckInterval: "10s",
			ConflictResolution:  "last-write-wins",
		},
		Kafka: KafkaConfig{
			Enabled:       false,
			Topic:         "chat-events",
			ConsumerGroup: "chat-platform",
			Compression:   "snappy",
			BatchSize:     1000,
			FlushInterval: "100ms",
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

// loadFromVault loads configuration from HashiCorp Vault
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
	// In a real implementation, you would use the Vault API client
	// For now, return defaults with a note that Vault integration is needed
	cfg, err := loadDefaults()
	if err != nil {
		return nil, err
	}

	// Override with Vault secrets if available
	// TODO: Implement actual Vault client integration
	// Example:
	// client, err := vault.NewClient(&vault.Config{Address: vaultConfig.Address})
	// secrets, err := client.Logical().Read(vaultConfig.SecretPath)
	// Apply secrets to config...

	return cfg, nil
}

// loadFromKubernetes loads configuration from Kubernetes secrets and configmaps
func loadFromKubernetes() (*Config, error) {
	cfg, err := loadDefaults()
	if err != nil {
		return nil, err
	}

	// Load from Kubernetes secrets mounted as files
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

	// JWT secret
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
	if redisAddresses := os.Getenv("REDIS_ADDRESSES"); redisAddresses != "" {
		cfg.Redis.Addresses = strings.Split(redisAddresses, ",")
	}

	return cfg, nil
}

// loadFromHelm loads configuration from Helm values
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

// ShardURL returns the PostgreSQL connection string for a specific shard
func (c *DatabaseConfig) ShardURL(shard ShardConfig) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		c.User, c.Password, shard.Host, shard.Port, shard.Database, c.SSLMode)
}

// ReplicaURL returns the PostgreSQL connection string for a specific replica
func (c *DatabaseConfig) ReplicaURL(replica ReplicaConfig) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		c.User, c.Password, replica.Host, replica.Port, replica.Database, c.SSLMode)
}

// GetReadTimeout returns the parsed read timeout duration
func (c *GatewayConfig) GetReadTimeout() time.Duration {
	if duration, err := time.ParseDuration(c.ReadTimeout); err == nil {
		return duration
	}
	return 15 * time.Second // default
}

// GetWriteTimeout returns the parsed write timeout duration
func (c *GatewayConfig) GetWriteTimeout() time.Duration {
	if duration, err := time.ParseDuration(c.WriteTimeout); err == nil {
		return duration
	}
	return 15 * time.Second // default
}

// GetTTL returns the parsed TTL duration
func (c *PresenceConfig) GetTTL() time.Duration {
	if duration, err := time.ParseDuration(c.TTL); err == nil {
		return duration
	}
	return 30 * time.Second // default
}

// GetConnMaxLifetime returns the parsed connection max lifetime duration
func (c *DatabaseConfig) GetConnMaxLifetime() time.Duration {
	if duration, err := time.ParseDuration(c.ConnMaxLifetime); err == nil {
		return duration
	}
	return time.Hour // default
}

// GetInterval returns the parsed interval duration
func (c *ServiceDiscoveryConfig) GetInterval() time.Duration {
	if duration, err := time.ParseDuration(c.Interval); err == nil {
		return duration
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

// GetFailoverTimeout returns the parsed failover timeout duration
func (c *MultiRegionConfig) GetFailoverTimeout() time.Duration {
	if duration, err := time.ParseDuration(c.FailoverTimeout); err == nil {
		return duration
	}
	return 30 * time.Second // default
}

// GetHealthCheckInterval returns the parsed health check interval duration
func (c *MultiRegionConfig) GetHealthCheckInterval() time.Duration {
	if duration, err := time.ParseDuration(c.HealthCheckInterval); err == nil {
		return duration
	}
	return 10 * time.Second // default
}

// GetFlushInterval returns the parsed flush interval duration
func (c *KafkaConfig) GetFlushInterval() time.Duration {
	if duration, err := time.ParseDuration(c.FlushInterval); err == nil {
		return duration
	}
	return 100 * time.Millisecond // default
}
