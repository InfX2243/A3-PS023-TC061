// Package cloudhypervisor provides a bounded client for Cloud Hypervisor's
// local HTTP API over a Unix-domain socket.
package cloudhypervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const apiPrefix = "/api/v1"

var ErrInvalidConfig = errors.New("invalid Cloud Hypervisor VM config")

// APIError describes a failed request without exposing unbounded response bodies.
type APIError struct {
	Operation string
	Code      string
	Status    int
	Message   string
}

func (e *APIError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("cloud hypervisor %s failed (http %d, %s): %s", e.Operation, e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("cloud hypervisor %s failed (%s): %s", e.Operation, e.Code, e.Message)
}

// Client talks to one Cloud Hypervisor instance over a local Unix socket.
type Client struct {
	httpClient *http.Client
}

func NewClient(socketPath string, timeout time.Duration) (*Client, error) {
	if strings.TrimSpace(socketPath) == "" {
		return nil, errors.New("Cloud Hypervisor socket path must not be empty")
	}
	if timeout <= 0 {
		return nil, errors.New("Cloud Hypervisor request timeout must be positive")
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
		DisableKeepAlives: true,
	}
	return &Client{httpClient: &http.Client{Transport: transport, Timeout: timeout}}, nil
}

// NewClientWithHTTPClient supports deterministic fake-API tests. Production
// callers should use NewClient to ensure traffic uses a Unix socket.
func NewClientWithHTTPClient(client *http.Client) (*Client, error) {
	if client == nil {
		return nil, errors.New("HTTP client must not be nil")
	}
	return &Client{httpClient: client}, nil
}

type CPUConfig struct {
	BootVCPUs int `json:"boot_vcpus"`
	MaxVCPUs  int `json:"max_vcpus"`
}

type MemoryConfig struct {
	Size string `json:"size"`
}

type PayloadConfig struct {
	Kernel    string `json:"kernel,omitempty"`
	Firmware  string `json:"firmware,omitempty"`
	Initramfs string `json:"initramfs,omitempty"`
	Cmdline   string `json:"cmdline,omitempty"`
}

type DiskConfig struct {
	Path      string `json:"path"`
	ImageType string `json:"image_type,omitempty"`
	ReadOnly  bool   `json:"readonly,omitempty"`
}

type RNGConfig struct {
	Source string `json:"src,omitempty"`
}

// VMConfig models the subset of Cloud Hypervisor's API used by this MVP.
// Image allowlisting and fixed path resolution belong in the worker layer.
type VMConfig struct {
	CPUs    CPUConfig      `json:"cpus"`
	Memory  MemoryConfig   `json:"memory,omitempty"`
	Payload PayloadConfig  `json:"payload"`
	Disks   []DiskConfig   `json:"disks,omitempty"`
	RNG     *RNGConfig     `json:"rng,omitempty"`
}

type VMStatus string

const (
	StatusCreated  VMStatus = "Created"
	StatusRunning  VMStatus = "Running"
	StatusShutdown VMStatus = "Shutdown"
	StatusPaused   VMStatus = "Paused"
)

type StatusResponse struct {
	State VMStatus `json:"state"`
}

func (c *Client) Prepare(ctx context.Context, config VMConfig) error {
	return c.Create(ctx, config)
}

func (c *Client) Create(ctx context.Context, config VMConfig) error {
	if config.CPUs.BootVCPUs < 1 || config.CPUs.MaxVCPUs < config.CPUs.BootVCPUs {
		return fmt.Errorf("%w: boot_vcpus must be positive and max_vcpus must be >= boot_vcpus", ErrInvalidConfig)
	}
	if strings.TrimSpace(config.Payload.Kernel) == "" && strings.TrimSpace(config.Payload.Firmware) == "" {
		return fmt.Errorf("%w: payload.kernel or payload.firmware is required", ErrInvalidConfig)
	}
	body, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("%w: encode request: %v", ErrInvalidConfig, err)
	}
	return c.request(ctx, "create", http.MethodPut, "/vm.create", body, http.StatusNoContent, nil)
}

func (c *Client) Start(ctx context.Context) error {
	return c.request(ctx, "start", http.MethodPut, "/vm.boot", nil, http.StatusNoContent, nil)
}

func (c *Client) Stop(ctx context.Context) error {
	return c.request(ctx, "stop", http.MethodPut, "/vm.shutdown", nil, http.StatusNoContent, nil)
}

func (c *Client) Status(ctx context.Context) (StatusResponse, error) {
	var result StatusResponse
	if err := c.request(ctx, "status", http.MethodGet, "/vm.info", nil, http.StatusOK, &result); err != nil {
		return StatusResponse{}, err
	}
	switch result.State {
	case StatusCreated, StatusRunning, StatusShutdown, StatusPaused:
		return result, nil
	default:
		return StatusResponse{}, &APIError{Operation: "status", Code: "invalid_response", Message: "response contains an unknown VM state"}
	}
}

func (c *Client) Ping(ctx context.Context) error {
	return c.request(ctx, "ping", http.MethodGet, "/vmm.ping", nil, http.StatusOK, nil)
}

func (c *Client) request(ctx context.Context, operation, method, endpoint string, body []byte, expectedStatus int, output any) error {
	if ctx == nil {
		return &APIError{Operation: operation, Code: "invalid_context", Message: "context must not be nil"}
	}
	url := "http://cloud-hypervisor" + apiPrefix + endpoint
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return &APIError{Operation: operation, Code: "request_build_failed", Message: "could not build request"}
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		code := "transport_error"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = "timeout"
		} else if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			code = "canceled"
		}
		return &APIError{Operation: operation, Code: code, Message: "request to local Cloud Hypervisor API did not complete"}
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if readErr != nil {
		return &APIError{Operation: operation, Code: "response_read_failed", Status: resp.StatusCode, Message: "could not read API response"}
	}
	if len(responseBody) > 64*1024 {
		return &APIError{Operation: operation, Code: "response_too_large", Status: resp.StatusCode, Message: "API response exceeded 64 KiB"}
	}
	if resp.StatusCode != expectedStatus {
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		if len(message) > 512 {
			message = message[:512]
		}
		return &APIError{Operation: operation, Code: "http_error", Status: resp.StatusCode, Message: message}
	}
	if output != nil {
		if err := json.Unmarshal(responseBody, output); err != nil {
			return &APIError{Operation: operation, Code: "invalid_response", Status: resp.StatusCode, Message: "API returned invalid JSON"}
		}
	}
	return nil
}
