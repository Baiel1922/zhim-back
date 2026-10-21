package config_test

import (
	"testing"

	"github.com/Baiel1922/zhim-back/backend/internal/config"
)

func setRequired(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("S3_BUCKET", "zhim")
	t.Setenv("IMGPROXY_URL", "http://img")
	t.Setenv("IMGPROXY_KEY", "aa")
	t.Setenv("IMGPROXY_SALT", "bb")
}

func TestLoadLists(t *testing.T) {
	setRequired(t)
	t.Setenv("HTTP_TRUSTED_PROXY_CIDRS", "")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://zhim.kg,https://admin.zhim.kg")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	// An empty CIDR would make the client IP middleware panic at startup.
	if len(cfg.HTTP.TrustedProxyCIDRs) != 0 {
		t.Fatalf("trusted proxies = %q, want none", cfg.HTTP.TrustedProxyCIDRs)
	}
	if len(cfg.CORSAllowedOrigins) != 2 || cfg.CORSAllowedOrigins[1] != "https://admin.zhim.kg" {
		t.Fatalf("origins = %q", cfg.CORSAllowedOrigins)
	}
	if cfg.RateLimit.CheckoutPerMinute != 10 || cfg.RateLimit.LookupPerMinute != 60 {
		t.Fatalf("rate limit defaults = %+v", cfg.RateLimit)
	}
}

func TestLoadRequiresSecrets(t *testing.T) {
	setRequired(t)
	t.Setenv("IMGPROXY_KEY", "")
	if _, err := config.Load(); err == nil {
		t.Fatal("want error when IMGPROXY_KEY is empty")
	}
}
