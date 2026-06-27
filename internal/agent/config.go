// Package agent orchestrates the agent's main loop: discover LH partitions,
// pull the per-account cursor, read lh.db, push to the platform.
package agent

import (
	"fmt"
	"os"
	"strconv"
)

// Config is loaded from environment variables. We deliberately avoid a config
// file so the deployment (systemd unit, launchd plist, NSSM service) owns the
// truth about how the agent runs.
type Config struct {
	APIEndpoint      string
	APIKey           string
	PartitionsDir    string
	DisableKeepAlive bool
}

const envPrefix = "LHA_"

// defaultAPIEndpoint is used when LHA_API_ENDPOINT is not set so a stock
// install talks to production without extra configuration.
const defaultAPIEndpoint = "https://api.analytics.leadget.ai"

// LoadConfig validates env vars and returns a usable Config or an error
// listing exactly what's wrong — installers parse this so the message must
// be self-contained.
func LoadConfig() (*Config, error) {
	endpoint := os.Getenv(envPrefix + "API_ENDPOINT")
	if endpoint == "" {
		endpoint = defaultAPIEndpoint
	}

	apiKey := os.Getenv(envPrefix + "API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("%sAPI_KEY is required", envPrefix)
	}

	partitionsDir := os.Getenv(envPrefix + "PARTITIONS_DIR")
	if partitionsDir == "" {
		return nil, fmt.Errorf("%sPARTITIONS_DIR is required (path to Linked Helper Partitions folder)", envPrefix)
	}

	disableKeepAlive, _ := strconv.ParseBool(os.Getenv(envPrefix + "DISABLE_KEEP_ALIVE"))

	return &Config{
		APIEndpoint:      endpoint,
		APIKey:           apiKey,
		PartitionsDir:    partitionsDir,
		DisableKeepAlive: disableKeepAlive,
	}, nil
}
