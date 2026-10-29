package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config contains non-secret controller settings. Credentials are supplied
// through environment variables and are deliberately excluded from JSON files.
type WorkerEndpoint struct {
	ID string `json:"id"`
	Name string `json:"name"`
	Endpoint string `json:"endpoint"`
	MemoryLimitMiB uint64 `json:"memory_limit_mib"`
}

type Config struct {
	ListenAddr                string   `json:"listen_addr"`
	DatabasePath              string   `json:"database_path"`
	MaxVCPUs                  int      `json:"max_vcpus"`
	MaxMemoryMiB              uint64   `json:"max_memory_mib"`
	MemorySafetyReserveMiB    uint64   `json:"memory_safety_reserve_mib"`
	AllowedImages             []string `json:"allowed_images"`
	Workers                   []WorkerEndpoint `json:"workers"`
	ShutdownTimeoutSeconds    int      `json:"shutdown_timeout_seconds"`
	OperatorToken             string   `json:"-"`
	WorkerSharedSecret        string   `json:"-"`
}

func Defaults() Config {
	return Config{
		ListenAddr:             "127.0.0.1:8080",
		DatabasePath:           "./data/controller.db",
		MaxVCPUs:               8,
		MaxMemoryMiB:           8192,
		MemorySafetyReserveMiB: 1024,
		AllowedImages:          []string{"ubuntu-demo"},
		Workers:                []WorkerEndpoint{},
		ShutdownTimeoutSeconds: 10,
	}
}

// Load reads a JSON configuration file, then applies supported environment
// overrides. An empty path uses safe defaults. Unknown JSON fields are rejected.
func Load(path string) (Config, error) {
	cfg := Defaults()
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return Config{}, fmt.Errorf("open config: %w", err)
		}
		defer f.Close()

		dec := json.NewDecoder(f)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			return Config{}, fmt.Errorf("decode config: %w", err)
		}
		if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return Config{}, errors.New("decode config: expected exactly one JSON object")
		}
	}

	if v, ok := os.LookupEnv("VM_CONTROLLER_LISTEN_ADDR"); ok {
		cfg.ListenAddr = strings.TrimSpace(v)
	}
	if v, ok := os.LookupEnv("VM_CONTROLLER_DATABASE_PATH"); ok {
		cfg.DatabasePath = strings.TrimSpace(v)
	}
	if v, ok := os.LookupEnv("VM_CONTROLLER_OPERATOR_TOKEN"); ok {
		cfg.OperatorToken = v
	}
	if v, ok := os.LookupEnv("VM_CONTROLLER_WORKER_SHARED_SECRET"); ok {
		cfg.WorkerSharedSecret = v
	}
	if v, ok := os.LookupEnv("VM_CONTROLLER_MAX_VCPUS"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("VM_CONTROLLER_MAX_VCPUS must be an integer: %w", err)
		}
		cfg.MaxVCPUs = n
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ListenAddr) == "" {
		return errors.New("listen_addr must not be empty")
	}
	if strings.TrimSpace(c.DatabasePath) == "" {
		return errors.New("database_path must not be empty")
	}
	if c.MaxVCPUs < 1 || c.MaxVCPUs > 256 {
		return errors.New("max_vcpus must be between 1 and 256")
	}
	if c.MaxMemoryMiB == 0 || c.MaxMemoryMiB > 1<<20 {
		return errors.New("max_memory_mib must be between 1 and 1048576")
	}
	if c.MemorySafetyReserveMiB >= c.MaxMemoryMiB {
		return errors.New("memory_safety_reserve_mib must be less than max_memory_mib")
	}
	if c.ShutdownTimeoutSeconds < 1 || c.ShutdownTimeoutSeconds > 300 {
		return errors.New("shutdown_timeout_seconds must be between 1 and 300")
	}
	if len(c.AllowedImages) == 0 {
		return errors.New("allowed_images must contain at least one image ID")
	}
	seen := make(map[string]struct{}, len(c.AllowedImages))
	for _, image := range c.AllowedImages {
		if image == "" || strings.TrimSpace(image) != image {
			return errors.New("allowed image IDs must be non-empty and have no surrounding whitespace")
		}
		if strings.ContainsAny(image, "/\\") || image == "." || image == ".." {
			return fmt.Errorf("allowed image ID %q contains a path-like value", image)
		}
		if _, exists := seen[image]; exists {
			return fmt.Errorf("duplicate allowed image ID %q", image)
		}
		seen[image] = struct{}{}
	}
	workerIDs := make(map[string]struct{}, len(c.Workers))
	idPattern := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config contains non-secret controller settings. Credentials are supplied
// through environment variables and are deliberately excluded from JSON files.
type WorkerEndpoint struct {
	ID string `json:"id"`
	Name string `json:"name"`
	Endpoint string `json:"endpoint"`
	MemoryLimitMiB uint64 `json:"memory_limit_mib"`
}

type Config struct {
	ListenAddr                string   `json:"listen_addr"`
	DatabasePath              string   `json:"database_path"`
	MaxVCPUs                  int      `json:"max_vcpus"`
	MaxMemoryMiB              uint64   `json:"max_memory_mib"`
	MemorySafetyReserveMiB    uint64   `json:"memory_safety_reserve_mib"`
	AllowedImages             []string `json:"allowed_images"`
	Workers                   []WorkerEndpoint `json:"workers"`
	ShutdownTimeoutSeconds    int      `json:"shutdown_timeout_seconds"`
	OperatorToken             string   `json:"-"`
	WorkerSharedSecret        string   `json:"-"`
}

func Defaults() Config {
	return Config{
		ListenAddr:             "127.0.0.1:8080",
		DatabasePath:           "./data/controller.db",
		MaxVCPUs:               8,
		MaxMemoryMiB:           8192,
		MemorySafetyReserveMiB: 1024,
		AllowedImages:          []string{"ubuntu-demo"},
		Workers:                []WorkerEndpoint{},
		ShutdownTimeoutSeconds: 10,
	}
}

// Load reads a JSON configuration file, then applies supported environment
// overrides. An empty path uses safe defaults. Unknown JSON fields are rejected.
func Load(path string) (Config, error) {
	cfg := Defaults()
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return Config{}, fmt.Errorf("open config: %w", err)
		}
		defer f.Close()

		dec := json.NewDecoder(f)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			return Config{}, fmt.Errorf("decode config: %w", err)
		}
		if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return Config{}, errors.New("decode config: expected exactly one JSON object")
		}
	}

	if v, ok := os.LookupEnv("VM_CONTROLLER_LISTEN_ADDR"); ok {
		cfg.ListenAddr = strings.TrimSpace(v)
	}
	if v, ok := os.LookupEnv("VM_CONTROLLER_DATABASE_PATH"); ok {
		cfg.DatabasePath = strings.TrimSpace(v)
	}
	if v, ok := os.LookupEnv("VM_CONTROLLER_OPERATOR_TOKEN"); ok {
		cfg.OperatorToken = v
	}
	if v, ok := os.LookupEnv("VM_CONTROLLER_WORKER_SHARED_SECRET"); ok {
		cfg.WorkerSharedSecret = v
	}
	if v, ok := os.LookupEnv("VM_CONTROLLER_MAX_VCPUS"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("VM_CONTROLLER_MAX_VCPUS must be an integer: %w", err)
		}
		cfg.MaxVCPUs = n
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ListenAddr) == "" {
		return errors.New("listen_addr must not be empty")
	}
	if strings.TrimSpace(c.DatabasePath) == "" {
		return errors.New("database_path must not be empty")
	}
	if c.MaxVCPUs < 1 || c.MaxVCPUs > 256 {
		return errors.New("max_vcpus must be between 1 and 256")
	}
	if c.MaxMemoryMiB == 0 || c.MaxMemoryMiB > 1<<20 {
		return errors.New("max_memory_mib must be between 1 and 1048576")
	}
	if c.MemorySafetyReserveMiB >= c.MaxMemoryMiB {
		return errors.New("memory_safety_reserve_mib must be less than max_memory_mib")
	}
	if c.ShutdownTimeoutSeconds < 1 || c.ShutdownTimeoutSeconds > 300 {
		return errors.New("shutdown_timeout_seconds must be between 1 and 300")
	}
	if len(c.AllowedImages) == 0 {
		return errors.New("allowed_images must contain at least one image ID")
	}
	seen := make(map[string]struct{}, len(c.AllowedImages))
	for _, image := range c.AllowedImages {
		if image == "" || strings.TrimSpace(image) != image {
			return errors.New("allowed image IDs must be non-empty and have no surrounding whitespace")
		}
		if strings.ContainsAny(image, "/\\") || image == "." || image == ".." {
			return fmt.Errorf("allowed image ID %q contains a path-like value", image)
		}
		if _, exists := seen[image]; exists {
			return fmt.Errorf("duplicate allowed image ID %q", image)
		}
)
	for _, worker := range c.Workers {
		if !idPattern.MatchString(worker.ID) || strings.TrimSpace(worker.Name) == "" || len(worker.Name) > 128 {
			return errors.New("worker IDs must be safe identifiers and worker names must be between 1 and 128 characters")
		}
		if _, exists := workerIDs[worker.ID]; exists { return fmt.Errorf("duplicate worker ID %q", worker.ID) }
		workerIDs[worker.ID] = struct{}{}
		u, err := url.Parse(worker.Endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("worker %q endpoint must be an absolute http(s) URL without credentials, query, or fragment", worker.ID)
		}
		if worker.MemoryLimitMiB == 0 || worker.MemoryLimitMiB > 1<<20 {
			return fmt.Errorf("worker %q memory_limit_mib must be between 1 and 1048576", worker.ID)
		}
	}
	return nil
}

func (c Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.ShutdownTimeoutSeconds) * time.Second
}
