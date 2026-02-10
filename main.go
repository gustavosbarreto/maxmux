package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Port        int      `yaml:"port"`
	Upstream    string   `yaml:"upstream"`
	OAuthToken  string   `yaml:"oauth_token"`
	VirtualKeys []string `yaml:"virtual_keys"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.Port == 0 {
		cfg.Port = 4000
	}
	if cfg.Upstream == "" {
		cfg.Upstream = "https://api.anthropic.com"
	}
	return &cfg, nil
}

func maskToken(t string) string {
	if len(t) <= 16 {
		return "***"
	}
	return t[:12] + "..." + t[len(t)-6:]
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
	flag.Parse()

	// Setup zerolog with console writer.
	level, err := zerolog.ParseLevel(*logLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.DateTime}).
		With().Timestamp().Logger().Level(level)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	if cfg.OAuthToken == "" {
		log.Fatal().Msg("oauth_token is required in config")
	}

	validKeys := make(map[string]bool, len(cfg.VirtualKeys))
	for _, k := range cfg.VirtualKeys {
		validKeys[k] = true
	}

	upstream, err := url.Parse(cfg.Upstream)
	if err != nil {
		log.Fatal().Err(err).Str("upstream", cfg.Upstream).Msg("invalid upstream URL")
	}

	oauthToken := cfg.OAuthToken
	log.Info().
		Int("port", cfg.Port).
		Str("upstream", cfg.Upstream).
		Int("virtual_keys", len(validKeys)).
		Str("oauth_token", maskToken(oauthToken)).
		Msg("starting maxmux")

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = upstream.Scheme
			req.URL.Host = upstream.Host
			req.Host = upstream.Host

			// Replace virtual key with real OAuth token in Authorization header.
			req.Header.Set("Authorization", "Bearer "+oauthToken)

			// Remove x-api-key if present — OAuth uses Authorization, not x-api-key.
			req.Header.Del("X-Api-Key")

			// Required headers for OAuth authentication (per PR #19453).
			if existing := req.Header.Get("Anthropic-Beta"); existing != "" {
				req.Header.Set("Anthropic-Beta", existing+",oauth-2025-04-20")
			} else {
				req.Header.Set("Anthropic-Beta", "oauth-2025-04-20")
			}
			req.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")

			log.Debug().
				Str("authorization", "Bearer "+maskToken(oauthToken)).
				Str("anthropic-beta", req.Header.Get("Anthropic-Beta")).
				Msg("injected oauth headers")
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Error().Err(err).Str("method", r.Method).Str("path", r.URL.Path).Msg("upstream error")
			http.Error(w, `{"error":{"message":"upstream error","type":"proxy_error"}}`, http.StatusBadGateway)
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Extract virtual key from Authorization Bearer header.
		var virtualKey string
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			virtualKey = strings.TrimPrefix(auth, "Bearer ")
		}

		if !validKeys[virtualKey] {
			log.Warn().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("remote", r.RemoteAddr).
				Msg("rejected — invalid virtual key")
			http.Error(w, `{"error":{"message":"invalid virtual key","type":"authentication_error"}}`, http.StatusUnauthorized)
			return
		}

		log.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Msg("forwarding")

		// Log all headers at debug level.
		for name, values := range r.Header {
			for _, v := range values {
				if strings.EqualFold(name, "Authorization") {
					log.Debug().Str("header", name).Str("value", maskToken(v)).Msg("request header")
				} else {
					log.Debug().Str("header", name).Str("value", v).Msg("request header")
				}
			}
		}

		proxy.ServeHTTP(w, r)

		log.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Dur("duration", time.Since(start)).
			Msg("completed")
	})

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Info().Str("addr", addr).Msg("listening")
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal().Err(err).Msg("server error")
	}
}
