//go:build unit

package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBPSImagesFromEnvironment(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("BPS_IMAGES_PUBLIC_BASE_URL", "http://192.0.2.1:8080")
	t.Setenv("BPS_IMAGES_DATA_DIR", "/var/lib/sub2api/bps-images")
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "http://192.0.2.1:8080", cfg.BPSImages.PublicBaseURL)
	require.Equal(t, "/var/lib/sub2api/bps-images", cfg.BPSImages.DataDir)
}
