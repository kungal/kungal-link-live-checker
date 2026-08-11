package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr         string
	APIKeys      []string
	CheckTimeout time.Duration
	BatchMax     int

	TTLAlive   time.Duration
	TTLDead    time.Duration
	TTLUnknown time.Duration

	RateRPS   float64
	RateBurst int

	QuarkBlockedAsDead bool

	UCEnabled  bool
	UCTokenURL string

	BaiduEnabled bool

	CaiyunEnabled bool
	Pan123Enabled bool
}

func Load() Config {
	return Config{
		Addr:               env("LLC_ADDR", ":6734"),
		APIKeys:            splitNonEmpty(env("LLC_API_KEYS", "")),
		CheckTimeout:       envDuration("LLC_CHECK_TIMEOUT", 8*time.Second),
		BatchMax:           envInt("LLC_BATCH_MAX", 50),
		TTLAlive:           envDuration("LLC_CACHE_TTL_ALIVE", 12*time.Hour),
		TTLDead:            envDuration("LLC_CACHE_TTL_DEAD", 7*24*time.Hour),
		TTLUnknown:         envDuration("LLC_CACHE_TTL_UNKNOWN", 0),
		RateRPS:            envFloat("LLC_RATE_RPS", 5),
		RateBurst:          envInt("LLC_RATE_BURST", 5),
		QuarkBlockedAsDead: envBool("LLC_QUARK_BLOCKED_AS_DEAD", true),
		UCEnabled:          envBool("LLC_UC_ENABLED", true),
		UCTokenURL:         env("LLC_UC_TOKEN_URL", "https://pc-api.uc.cn/1/clouddrive/share/sharepage/token?pr=UCBrowser&fr=pc"),
		BaiduEnabled:       envBool("LLC_BAIDU_ENABLED", true),
		CaiyunEnabled:      envBool("LLC_CAIYUN_ENABLED", true),
		Pan123Enabled:      envBool("LLC_PAN123_ENABLED", true),
	}
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return def
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
