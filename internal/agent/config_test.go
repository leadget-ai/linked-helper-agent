package agent

import (
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	t.Run("all required vars set", func(t *testing.T) {
		t.Setenv("LHA_API_ENDPOINT", "https://api.example.test")
		t.Setenv("LHA_API_KEY", "secret")
		t.Setenv("LHA_PARTITIONS_DIR", "/tmp/partitions")
		t.Setenv("LHA_DISABLE_KEEP_ALIVE", "true")

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.APIEndpoint != "https://api.example.test" || cfg.APIKey != "secret" || cfg.PartitionsDir != "/tmp/partitions" {
			t.Errorf("cfg = %+v", cfg)
		}
		if !cfg.DisableKeepAlive {
			t.Errorf("DisableKeepAlive = false, want true")
		}
	})

	missing := []struct {
		name     string
		endpoint string
		key      string
		partDir  string
	}{
		{"no endpoint", "", "k", "/d"},
		{"no key", "https://x", "", "/d"},
		{"no partitions dir", "https://x", "k", ""},
	}
	for _, tc := range missing {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LHA_API_ENDPOINT", tc.endpoint)
			t.Setenv("LHA_API_KEY", tc.key)
			t.Setenv("LHA_PARTITIONS_DIR", tc.partDir)
			if _, err := LoadConfig(); err == nil {
				t.Errorf("LoadConfig() err = nil, want error for %s", tc.name)
			}
		})
	}
}

func TestReportIntervalClamp(t *testing.T) {
	a := &Agent{}
	cases := []struct {
		set  time.Duration
		want time.Duration
	}{
		{10 * time.Second, minReportInterval}, // below floor
		{2 * time.Hour, maxReportInterval},    // above ceiling
		{5 * time.Minute, 5 * time.Minute},    // in range
	}
	for _, tc := range cases {
		a.setReportInterval(tc.set)
		if got := a.getReportInterval(); got != tc.want {
			t.Errorf("setReportInterval(%v) → %v, want %v", tc.set, got, tc.want)
		}
	}
}
