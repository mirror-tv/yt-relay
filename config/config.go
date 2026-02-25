package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Conf struct {
	// AppName is only allowed to have alphanumeric, dash, and dot.
	AppName    string        `mapstructure:"appName"`
	Address    string        `mapstructure:"address"`
	ApiKey     string        `mapstructure:"apiKey"`
	Cache      Cache         `mapstructure:"cache"`
	CmsURL     string        `mapstructure:"cmsUrl"`
	Port       int           `mapstructure:"port"`
	Redis      *RedisService `mapstructure:"redis"`
	Whitelists Whitelists    `mapstructure:"whitelists"`
}

// Whitelists are maps, key is the whitelist string, value determines if it should be effective
type Whitelists struct {
	ChannelIDs  map[string]bool `mapstructure:"channelIDs"`
	PlaylistIDs map[string]bool `mapstructure:"playlistIDs"`
}

type Cache struct {
	IsEnabled    bool            `mapstructure:"isEnabled"`
	DisabledAPIs map[string]bool `mapstructure:"disabledApis"`
	TTL          int             `mapstructure:"ttl"`
	ErrorTTL     int             `mapstructure:"errorTtl"`
	OverwriteTTL map[string]int  `mapstructure:"overwriteTtl"`
}

type OverwriteTTL struct {
	TTL       int    `mapstructure:"ttl"`
	PrefixAPI string `mapstructure:"apiPrefix"`
}

// RedisService defines the conf of redis for cache. User should find the right configuration according to the type
type RedisService struct {
	Type           RedisType              `mapstructure:"type"`
	Cluster        *RedisCluster          `mapstructure:"cluster"`
	SingleInstance *RedisSingleInstance   `mapstructure:"single"`
	Sentinel       *RedisSentinel         `mapstructure:"sentinel"`
	Replica        *RedisReplicaInstances `mapstructure:"replica"`
}

type RedisType string

const (
	Cluster  RedisType = "cluster"
	Single   RedisType = "single"
	Sentinel RedisType = "sentinel"
	Replica  RedisType = "replica"
)

type RedisCluster struct {
	Addrs    []RedisAddress `mapstructure:"addresses"`
	Password string         `mapstructure:"password"`
}

type RedisSingleInstance struct {
	Instance RedisAddress `mapstructure:"instance"`
	Password string       `mapstructure:"password"`
}

type RedisSentinel struct {
	Addrs    []RedisAddress `mapstructure:"addresses"`
	Password string         `mapstructure:"password"`
}

type RedisReplicaInstances struct {
	MasterAddrs []RedisAddress `mapstructure:"writers"`
	SlaveAddrs  []RedisAddress `mapstructure:"readers"`
	Password    string         `mapstructure:"password"`
}

type RedisAddress struct {
	Addr string `mapstructure:"address"`
	Port int    `mapstructure:"port"`
}

func (c *Conf) Valid() bool {

	isValidAppName, _ := regexp.MatchString("^[a-zA-Z0-9.-]+$", c.AppName)
	if !isValidAppName {
		log.Errorf("appName(%s) can only contains alphanumeric, dot, and hyphen are allowed, and it cannot be empty", c.AppName)
		return false
	}

	if c.ApiKey == "" {
		log.Error("apiKey cannot be empty")
		return false
	}

	if len(c.Whitelists.ChannelIDs) == 0 {
		log.Error("whitelist's channel id cannot be empty")
		return false
	}

	if c.CmsURL == "" {
		log.Error("cmsUrl cannot be empty")
		return false
	}

	if c.Cache.IsEnabled {
		if c.Cache.TTL <= 0 {
			log.Errorf("enabled cache's default ttl(%d) cannot be zero or negative", c.Cache.TTL)
			return false
		}

		if c.Cache.ErrorTTL <= 0 {
			log.Errorf("enabled cache's default error ttl(%d) cannot be zero or negative", c.Cache.ErrorTTL)
			return false
		}

		for api, ttl := range c.Cache.OverwriteTTL {
			if ttl <= 0 {
				log.Errorf("enabled cache's ttl(%d) fot api(%s) cannot be zero or negative", ttl, api)
				return false
			}
		}
	}

	if c.Redis != nil {
		redis := c.Redis
		switch redis.Type {
		case Cluster:
			if redis.Cluster == nil {
				log.Error("redis type is set to %s but there is no %s configuration", Cluster, Cluster)
				return false
			}

			cluster := redis.Cluster

			if len(cluster.Addrs) == 0 {
				log.Errorf("%s addresses cannot be empty", Cluster)
				return false
			}
			for _, addr := range cluster.Addrs {
				if len(addr.Addr) == 0 {
					log.Errorf("one of the %s addresses is empty", Cluster)
					return false
				}
			}
		case Single:
			if redis.SingleInstance == nil {
				log.Error("redis type is set to %s but there is no %s configuration", Single, Single)
				return false
			}

			single := redis.SingleInstance

			if single.Instance.Addr == "" {
				log.Errorf("%s address cannot be empty", Single)
				return false
			}
		case Sentinel:
			if redis.Sentinel == nil {
				log.Error("redis type is set to %s but there is no %s configuration", Sentinel, Sentinel)
				return false
			}

			sentinel := redis.Sentinel

			if len(sentinel.Addrs) == 0 {
				log.Errorf("%s addresses cannot be empty", Sentinel)
				return false
			}
			for _, addr := range sentinel.Addrs {
				if len(addr.Addr) == 0 {
					log.Errorf("one of the %s addresses is empty", Sentinel)
					return false
				}
			}
		case Replica:
			if redis.Replica == nil {
				log.Error("redis type is set to %s but there is no %s configuration", Replica, Replica)
				return false
			}

			replica := redis.Replica

			if len(replica.MasterAddrs) == 0 {
				log.Errorf("%s writer addresses cannot be empty", Replica)
				return false
			}
			for _, addr := range replica.MasterAddrs {
				if len(addr.Addr) == 0 {
					log.Errorf("one of the %s writer addresses is empty", Replica)
					return false
				}
			}

			if len(replica.SlaveAddrs) == 0 {
				log.Errorf("%s reader addresses cannot be empty", Replica)
				return false
			}
			for _, addr := range replica.SlaveAddrs {
				if len(addr.Addr) == 0 {
					log.Errorf("one of the %s reader addresses is empty", Replica)
					return false
				}
			}
		default:
			log.Errorf("redis type(%s) is not supported", redis.Type)
			return false
		}
	}

	return true
}

// parseAddresses parses a comma-separated list of "host:port" into []RedisAddress.
func parseAddresses(s string) ([]RedisAddress, error) {
	var addrs []RedisAddress
	for _, entry := range strings.Split(s, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid address format %q, expected host:port", entry)
		}
		port, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid port in address %q: %v", entry, err)
		}
		addrs = append(addrs, RedisAddress{Addr: parts[0], Port: port})
	}
	return addrs, nil
}

// parseCSVMap parses "key1:val1,key2:val2" into map[string]int.
func parseCSVMap(s string) (map[string]int, error) {
	m := make(map[string]int)
	for _, entry := range strings.Split(s, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid format %q, expected key:value", entry)
		}
		val, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("invalid integer value in %q: %v", entry, err)
		}
		m[strings.TrimSpace(parts[0])] = val
	}
	return m, nil
}

// parseCSVBoolMap parses "key1,key2" into map[string]bool with all values set to true.
func parseCSVBoolMap(s string) map[string]bool {
	m := make(map[string]bool)
	for _, entry := range strings.Split(s, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		m[entry] = true
	}
	return m
}

// loadComplexEnvVars populates fields that cannot be directly bound via Viper
// (CSV-formatted whitelists, redis addresses, cache overwrite TTLs).
func loadComplexEnvVars(cfg *Conf) error {
	// Whitelists
	if s := os.Getenv("WHITELIST_CHANNEL_IDS"); s != "" {
		cfg.Whitelists.ChannelIDs = parseCSVBoolMap(s)
	}

	// Cache extras
	if s := os.Getenv("CACHE_DISABLED_APIS"); s != "" {
		cfg.Cache.DisabledAPIs = parseCSVBoolMap(s)
	}
	if s := os.Getenv("CACHE_OVERWRITE_TTL"); s != "" {
		m, err := parseCSVMap(s)
		if err != nil {
			return fmt.Errorf("failed to parse CACHE_OVERWRITE_TTL: %v", err)
		}
		cfg.Cache.OverwriteTTL = m
	}

	// Redis
	if redisType := os.Getenv("REDIS_TYPE"); redisType != "" {
		password := os.Getenv("REDIS_PASSWORD")
		cfg.Redis = &RedisService{Type: RedisType(redisType)}

		switch RedisType(redisType) {
		case Single:
			addrs, err := parseAddresses(os.Getenv("REDIS_ADDRESSES"))
			if err != nil {
				return fmt.Errorf("failed to parse REDIS_ADDRESSES: %v", err)
			}
			if len(addrs) == 0 {
				return errors.New("REDIS_ADDRESSES is required for single redis type")
			}
			cfg.Redis.SingleInstance = &RedisSingleInstance{
				Instance: addrs[0],
				Password: password,
			}
		case Cluster:
			addrs, err := parseAddresses(os.Getenv("REDIS_ADDRESSES"))
			if err != nil {
				return fmt.Errorf("failed to parse REDIS_ADDRESSES: %v", err)
			}
			cfg.Redis.Cluster = &RedisCluster{
				Addrs:    addrs,
				Password: password,
			}
		case Sentinel:
			addrs, err := parseAddresses(os.Getenv("REDIS_ADDRESSES"))
			if err != nil {
				return fmt.Errorf("failed to parse REDIS_ADDRESSES: %v", err)
			}
			cfg.Redis.Sentinel = &RedisSentinel{
				Addrs:    addrs,
				Password: password,
			}
		case Replica:
			writers, err := parseAddresses(os.Getenv("REDIS_ADDRESSES"))
			if err != nil {
				return fmt.Errorf("failed to parse REDIS_ADDRESSES: %v", err)
			}
			readers, err := parseAddresses(os.Getenv("REDIS_READER_ADDRESSES"))
			if err != nil {
				return fmt.Errorf("failed to parse REDIS_READER_ADDRESSES: %v", err)
			}
			cfg.Redis.Replica = &RedisReplicaInstances{
				MasterAddrs: writers,
				SlaveAddrs:  readers,
				Password:    password,
			}
		}
	}

	return nil
}

// Load loads configuration. When configFile is provided, it reads from the YAML file.
// Otherwise, it reads from environment variables. Env vars always override file values
// for simple fields (appName, apiKey, address, port, cache settings).
func Load(configFile string) (*Conf, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("address", "0.0.0.0")
	v.SetDefault("port", 8080)
	v.SetDefault("cache.isEnabled", false)

	// Bind environment variables for simple fields
	_ = v.BindEnv("appName", "APP_NAME")
	_ = v.BindEnv("apiKey", "API_KEY")
	_ = v.BindEnv("address", "ADDRESS")
	_ = v.BindEnv("port", "PORT")
	_ = v.BindEnv("cmsUrl", "CMS_URL")
	_ = v.BindEnv("cache.isEnabled", "CACHE_ENABLED")
	_ = v.BindEnv("cache.ttl", "CACHE_TTL")
	_ = v.BindEnv("cache.errorTtl", "CACHE_ERROR_TTL")

	if configFile != "" {
		log.Printf("loading configuration file from %s", configFile)
		v.SetConfigFile(configFile)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("could not read configuration file: %v", err)
		}
	} else {
		log.Println("loading configuration from environment variables")
	}

	cfg := &Conf{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal configuration: %v", err)
	}

	// When no config file, parse complex types from env vars
	if configFile == "" {
		if err := loadComplexEnvVars(cfg); err != nil {
			return nil, err
		}
	}

	if !cfg.Valid() {
		return nil, errors.New("invalid configuration")
	}

	log.Println("configuration ok")
	return cfg, nil
}
