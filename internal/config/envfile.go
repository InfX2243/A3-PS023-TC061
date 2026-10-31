package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// LoadDotEnv loads KEY=VALUE lines from a local environment file without
// evaluating shell syntax. Variables already present in the process environment
// take precedence. A missing file is allowed so systemd/container deployments
// can provide environment variables directly.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) { return nil }
	if err != nil { return fmt.Errorf("open environment file: %w", err) }
	defer f.Close()
	info, err := f.Stat()
	if err != nil { return fmt.Errorf("stat environment file: %w", err) }
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("environment file must not be accessible by group or other users (use chmod 600)")
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") { continue }
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, " \t\r\n") {
			return fmt.Errorf("environment file line %d must be KEY=VALUE", lineNo)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1:len(value)-1]
		} else if i := strings.Index(value, " #"); i >= 0 {
			value = strings.TrimSpace(value[:i])
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil { return fmt.Errorf("set environment variable on line %d: %w", lineNo, err) }
		}
	}
	if err := scanner.Err(); err != nil { return fmt.Errorf("read environment file: %w", err) }
	return nil
}
