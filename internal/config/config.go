package config

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const defaultMaxEditableFileBytes int64 = 1 << 20

type Server struct {
	Listen    string `yaml:"listen"`
	TLSCert   string `yaml:"tls_cert"`
	TLSKey    string `yaml:"tls_key"`
	PublicURL string `yaml:"public_url"`
}

type Config struct {
	Server               Server `yaml:"server"`
	DataDir              string `yaml:"data_dir"`
	LogFormat            string `yaml:"log_format"`
	MaxEditableFileBytes int64  `yaml:"max_editable_file_bytes"`
}

func Default() Config {
	return Config{
		Server:               Server{Listen: "127.0.0.1:8080"},
		DataDir:              "/var/lib/porty",
		LogFormat:            "text",
		MaxEditableFileBytes: defaultMaxEditableFileBytes,
	}
}

func Load(args []string, lookupEnv func(string) (string, bool)) (Config, error) {
	flags := flag.NewFlagSet("porty", flag.ContinueOnError)
	configPath := flags.String("config", "", "configuration file")
	listen := flags.String("listen", "", "listen address")
	dataDir := flags.String("data-dir", "", "data directory")
	logFormat := flags.String("log-format", "", "log format")
	tlsCert := flags.String("tls-cert", "", "TLS certificate")
	tlsKey := flags.String("tls-key", "", "TLS key")
	publicURL := flags.String("public-url", "", "external public origin")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}

	cfg := Default()
	if *configPath != "" {
		contents, err := os.ReadFile(*configPath)
		if err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(contents, &cfg); err != nil {
			return Config{}, fmt.Errorf("decode config: %w", err)
		}
	}

	applyEnv := func(key string, destination *string) {
		if value, ok := lookupEnv(key); ok {
			*destination = value
		}
	}
	applyEnv("PORTY_LISTEN", &cfg.Server.Listen)
	applyEnv("PORTY_DATA_DIR", &cfg.DataDir)
	applyEnv("PORTY_LOG_FORMAT", &cfg.LogFormat)
	applyEnv("PORTY_TLS_CERT", &cfg.Server.TLSCert)
	applyEnv("PORTY_TLS_KEY", &cfg.Server.TLSKey)
	applyEnv("PORTY_PUBLIC_URL", &cfg.Server.PublicURL)
	if value, ok := lookupEnv("PORTY_MAX_EDITABLE_FILE_BYTES"); ok {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("PORTY_MAX_EDITABLE_FILE_BYTES: %w", err)
		}
		cfg.MaxEditableFileBytes = parsed
	}

	flags.Visit(func(current *flag.Flag) {
		switch current.Name {
		case "listen":
			cfg.Server.Listen = *listen
		case "data-dir":
			cfg.DataDir = *dataDir
		case "log-format":
			cfg.LogFormat = *logFormat
		case "tls-cert":
			cfg.Server.TLSCert = *tlsCert
		case "tls-key":
			cfg.Server.TLSKey = *tlsKey
		case "public-url":
			cfg.Server.PublicURL = *publicURL
		}
	})

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Server.Listen == "" {
		return errors.New("server.listen is required")
	}
	if c.DataDir == "" {
		return errors.New("data_dir is required")
	}
	if (c.Server.TLSCert == "") != (c.Server.TLSKey == "") {
		return errors.New("TLS certificate and key must be configured together")
	}
	if c.Server.PublicURL != "" {
		u, err := url.Parse(c.Server.PublicURL)
		if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" || u.Host != strings.ToLower(u.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.String() != c.Server.PublicURL || u.Scheme == "https" && u.Port() == "443" || u.Scheme == "http" && u.Port() == "80" {
			return errors.New("server.public_url must be a canonical HTTP origin")
		}
	}
	if c.LogFormat != "text" && c.LogFormat != "json" {
		return errors.New("log_format must be text or json")
	}
	if c.MaxEditableFileBytes <= 0 {
		return errors.New("max_editable_file_bytes must be positive")
	}
	return nil
}
