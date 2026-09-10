package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// PrismEndpoint is a Prism Central (or compatible) API target.
type PrismEndpoint struct {
	IP       string `yaml:"ip"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type Config struct {
	PrismCentralIP string          `yaml:"prism_central_ip"`
	PrismElementIP string          `yaml:"prism_element_ip"`
	PrismCentrals  []PrismEndpoint `yaml:"prism_centrals"`
	Username       string          `yaml:"username"`
	Password       string          `yaml:"password"`
	PollInterval   time.Duration   `yaml:"poll_interval"`
	RequestTimeout time.Duration   `yaml:"request_timeout"`
	Insecure       bool            `yaml:"insecure"`
}

// defaults is the configuration before any file, environment or flag is read.
// Insecure starts true because Prism ships self-signed certificates, so
// verifying against the system roots would fail every default run.
func defaults() *Config {
	return &Config{
		PollInterval:   5 * time.Second,
		RequestTimeout: 10 * time.Second,
		Insecure:       true,
	}
}

func Load() (*Config, error) {
	cfg := defaults()

	configPath := flag.String("config", "config.yaml", "path to config file")
	pcIP := flag.String("pc-ip", "", "Prism Central IP")
	peIP := flag.String("pe-ip", "", "Prism Element IP")
	verifyTLS := flag.Bool("verify-tls", false, "verify Prism TLS certificates against the system roots (off by default: Prism ships self-signed certificates)")
	flag.Parse()

	// Whether --verify-tls was actually passed, as opposed to sitting at its
	// default. Without this, the default would silently override `insecure`
	// from the config file.
	verifyTLSSet := false
	flag.Visit(func(fl *flag.Flag) {
		if fl.Name == "verify-tls" {
			verifyTLSSet = true
		}
	})

	if err := cfg.loadFile(*configPath); err != nil {
		// File is optional; only fail if explicitly specified and missing.
		if *configPath != "config.yaml" {
			return nil, fmt.Errorf("loading config file: %w", err)
		}
	}

	cfg.applyEnv()
	cfg.applyFlags(*pcIP, *peIP)
	cfg.applyTLSChoice(*verifyTLS, verifyTLSSet)

	return cfg, nil
}

// applyTLSChoice resolves certificate verification. The flag wins when it was
// passed; otherwise whatever the file or the environment set stands.
func (c *Config) applyTLSChoice(verifyTLS, explicit bool) {
	if explicit {
		c.Insecure = !verifyTLS
	}
}

func (c *Config) loadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, c)
}

func (c *Config) applyEnv() {
	if v := os.Getenv("NTNX_PC_IP"); v != "" {
		c.PrismCentralIP = v
	}
	if v := os.Getenv("NTNX_PE_IP"); v != "" {
		c.PrismElementIP = v
	}
	if v := os.Getenv("NTNX_USER"); v != "" {
		c.Username = v
	}
	if v := os.Getenv("NTNX_PASS"); v != "" {
		c.Password = v
	}
	if v := os.Getenv("NTNX_VERIFY_TLS"); v != "" {
		if verify, err := strconv.ParseBool(v); err == nil {
			c.Insecure = !verify
		}
	}
}

func (c *Config) applyFlags(pcIP, peIP string) {
	if pcIP != "" {
		c.PrismCentralIP = pcIP
	}
	if peIP != "" {
		c.PrismElementIP = peIP
	}
}

func (c *Config) HasPrismCentral() bool {
	return c.PrismCentralIP != "" || len(c.PrismCentrals) > 0
}

func (c *Config) HasAnyEndpoint() bool {
	return c.HasPrismCentral() || c.PrismElementIP != ""
}

func (c *Config) NeedsWizard() bool {
	return !c.HasAnyEndpoint()
}

// PCEndpoints returns configured Prism Central targets, filling blank
// per-entry credentials from the top-level username/password.
func (c *Config) PCEndpoints() []PrismEndpoint {
	if len(c.PrismCentrals) > 0 {
		out := make([]PrismEndpoint, len(c.PrismCentrals))
		copy(out, c.PrismCentrals)
		for i := range out {
			if out[i].Username == "" {
				out[i].Username = c.Username
			}
			if out[i].Password == "" {
				out[i].Password = c.Password
			}
		}
		return out
	}
	if c.PrismCentralIP != "" {
		return []PrismEndpoint{{
			IP:       c.PrismCentralIP,
			Username: c.Username,
			Password: c.Password,
		}}
	}
	return nil
}
