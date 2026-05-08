package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Listen != ":7890" {
		t.Errorf("expected listen :7890, got %s", cfg.Listen)
	}
	if cfg.API != ":9090" {
		t.Errorf("expected api :9090, got %s", cfg.API)
	}
	if cfg.HealthCheck.Interval != 300 {
		t.Errorf("expected health_check interval 300, got %d", cfg.HealthCheck.Interval)
	}
}

func TestLoad(t *testing.T) {
	content := []byte(`
listen: ":8080"
api: ":9091"
subscribe:
  - url: "https://test.com/sub"
    interval: 1800
    name: "test"
health_check:
  url: "https://test.com/204"
  interval: 120
  timeout: 3
  max_failures: 5
`)

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Listen != ":8080" {
		t.Errorf("expected listen :8080, got %s", cfg.Listen)
	}
	if cfg.API != ":9091" {
		t.Errorf("expected api :9091, got %s", cfg.API)
	}
	if len(cfg.Subscribe) != 1 {
		t.Fatalf("expected 1 subscribe, got %d", len(cfg.Subscribe))
	}
	if cfg.Subscribe[0].URL != "https://test.com/sub" {
		t.Errorf("expected subscribe url https://test.com/sub, got %s", cfg.Subscribe[0].URL)
	}
	if cfg.Subscribe[0].Interval != 1800 {
		t.Errorf("expected subscribe interval 1800, got %d", cfg.Subscribe[0].Interval)
	}
	if cfg.HealthCheck.MaxFailures != 5 {
		t.Errorf("expected max_failures 5, got %d", cfg.HealthCheck.MaxFailures)
	}
}

func TestLoadNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
