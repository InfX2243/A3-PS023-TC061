package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotEnvLoadsValuesWithoutOverridingProcessEnvironment(t *testing.T) {
	t.Setenv("AIO_TEST_EXISTING", "process-value")
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("# comment\nAIO_TEST_FILE=from-file\nAIO_TEST_EXISTING=file-value\nAIO_TEST_QUOTED=\"hello world\"\n"), 0600); err != nil { t.Fatal(err) }
	if err := LoadDotEnv(path); err != nil { t.Fatal(err) }
	if got := os.Getenv("AIO_TEST_FILE"); got != "from-file" { t.Fatalf("file value = %q", got) }
	if got := os.Getenv("AIO_TEST_EXISTING"); got != "process-value" { t.Fatalf("existing environment value = %q", got) }
	if got := os.Getenv("AIO_TEST_QUOTED"); got != "hello world" { t.Fatalf("quoted value = %q", got) }
}

func TestLoadDotEnvRejectsInsecurePermissionsAndMalformedLines(t *testing.T) {
	t.Run("insecure permissions", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(path, []byte("AIO_TEST=x\n"), 0644); err != nil { t.Fatal(err) }
		if err := LoadDotEnv(path); err == nil || !strings.Contains(err.Error(), "chmod 600") { t.Fatalf("error = %v", err) }
	})
	t.Run("malformed line", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(path, []byte("not-a-pair\n"), 0600); err != nil { t.Fatal(err) }
		if err := LoadDotEnv(path); err == nil { t.Fatal("expected malformed line error") }
	})
}

func TestLoadDotEnvMissingFileIsAllowed(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "missing.env")); err != nil { t.Fatalf("missing optional env file: %v", err) }
}
