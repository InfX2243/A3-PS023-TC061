package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/InfX2243/A3-PS023-TC061/internal/controllerapi"
)

type workerVMRequest struct {
	OperationID string `json:"operation_id"`
	VMID string `json:"vm_id"`
	Name string `json:"name"`
	VCPUs int `json:"vcpus"`
	MemoryBytes uint64 `json:"memory_bytes"`
	ImageID string `json:"image_id"`
}

type workerOperationRequest struct { OperationID string `json:"operation_id"` }

type workerVMReport struct {
	WorkerID string `json:"worker_id"`
	VMID string `json:"vm_id"`
	OperationID string `json:"operation_id"`
	ImageID string `json:"image_id"`
	VCPUs int `json:"vcpus"`
	MemoryBytes uint64 `json:"memory_bytes"`
	State string `json:"state"`
}

type workerCallError struct {
	Status int
	Unknown bool
	cause error
}
func (e *workerCallError) Error() string {
	if e.Unknown { return "worker outcome is unknown" }
	return fmt.Sprintf("worker returned HTTP %d", e.Status)
}
func (e *workerCallError) Unwrap() error { return e.cause }

type workerClient struct {
	http *http.Client
	secret string
}

func newWorkerClient(secret string, timeout time.Duration) *workerClient {
	if timeout <= 0 { timeout = 5 * time.Second }
	return &workerClient{http: &http.Client{Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("worker redirects are disabled") }}, secret: secret}
}

func (c *workerClient) prepare(ctx context.Context, endpoint string, req workerVMRequest) (workerVMReport, error) {
	var result workerVMReport
	err := c.call(ctx, endpoint, http.MethodPost, "/api/v1/worker/vms", req, &result)
	return result, err
}
func (c *workerClient) start(ctx context.Context, endpoint, vmID, operationID string) (workerVMReport, error) {
	var result workerVMReport
	err := c.call(ctx, endpoint, http.MethodPost, "/api/v1/worker/vms/"+vmID+"/start", workerOperationRequest{OperationID: operationID}, &result)
	return result, err
}
func (c *workerClient) stop(ctx context.Context, endpoint, vmID, operationID string) (workerVMReport, error) {
	var result workerVMReport
	err := c.call(ctx, endpoint, http.MethodPost, "/api/v1/worker/vms/"+vmID+"/stop", workerOperationRequest{OperationID: operationID}, &result)
	return result, err
}
func (c *workerClient) status(ctx context.Context, endpoint, vmID string) (workerVMReport, error) {
	var result workerVMReport
	err := c.call(ctx, endpoint, http.MethodGet, "/api/v1/worker/vms/"+vmID, nil, &result)
	return result, err
}

func (c *workerClient) call(ctx context.Context, endpoint, method, path string, body, output any) error {
	if c.secret == "" { return controllerapi.ErrUnavailable }
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" { return controllerapi.ErrUnavailable }
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil { return fmt.Errorf("encode worker request: %w", err) }
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint+path, reader)
	if err != nil { return fmt.Errorf("build worker request: %w", err) }
	req.Header.Set("Authorization", "Bearer "+c.secret)
	req.Header.Set("Accept", "application/json")
	if body != nil { req.Header.Set("Content-Type", "application/json") }
	resp, err := c.http.Do(req)
	if err != nil { return &workerCallError{Unknown: true, cause: err} }
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil { return &workerCallError{Unknown: true, cause: err} }
	if len(data) > 64*1024 { return &workerCallError{Status: resp.StatusCode, Unknown: resp.StatusCode >= 500} }
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &workerCallError{Status: resp.StatusCode, Unknown: resp.StatusCode >= 500}
	}
	if output != nil {
		if err := json.Unmarshal(data, output); err != nil { return &workerCallError{Status: resp.StatusCode, Unknown: true, cause: err} }
	}
	return nil
}
