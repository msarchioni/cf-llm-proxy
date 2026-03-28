package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var (
	version = "1.0.0"
)

type Config struct {
	ListenAddr     string
	TargetHost     string
	CFClientID     string
	CFClientSecret string
}

func loadConfig() (*Config, error) {
	listenPort := flag.Int("port", 8900, "Local listen port")
	listenAddr := flag.String("addr", "127.0.0.1", "Local listen address")
	targetHost := flag.String("target", "", "Target host (default: env CF_TARGET_HOST or llm.sark-ai.org)")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("cf-llm-proxy %s\n", version)
		os.Exit(0)
	}

	cfg := &Config{
		ListenAddr: fmt.Sprintf("%s:%d", *listenAddr, *listenPort),
	}

	// Target host
	if *targetHost != "" {
		cfg.TargetHost = *targetHost
	} else if env := os.Getenv("CF_TARGET_HOST"); env != "" {
		cfg.TargetHost = env
	} else {
		cfg.TargetHost = "llm.sark-ai.org"
	}

	// Ensure target has scheme
	if !strings.HasPrefix(cfg.TargetHost, "http") {
		cfg.TargetHost = "https://" + cfg.TargetHost
	}

	// Validate target URL
	if _, err := url.Parse(cfg.TargetHost); err != nil {
		return nil, fmt.Errorf("invalid target host: %w", err)
	}

	// CF credentials
	cfg.CFClientID = os.Getenv("CF_ACCESS_CLIENT_ID")
	cfg.CFClientSecret = os.Getenv("CF_ACCESS_CLIENT_SECRET")

	if cfg.CFClientID == "" || cfg.CFClientSecret == "" {
		return nil, fmt.Errorf("CF_ACCESS_CLIENT_ID and CF_ACCESS_CLIENT_SECRET must be set")
	}

	return cfg, nil
}

// proxyHandler creates an http.Handler that proxies requests to the target,
// injecting Cloudflare Zero Trust headers and streaming responses byte-by-byte.
func proxyHandler(cfg *Config) http.Handler {
	targetURL, _ := url.Parse(cfg.TargetHost)

	// HTTP client with long timeout for LLM streaming responses
	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 300 * time.Second,
		// Disable compression so we get raw SSE stream
		DisableCompression: true,
	}

	client := &http.Client{
		Transport: transport,
		// No timeout on client level - we handle it per-request via context
		Timeout: 0,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// Build upstream URL
		upstream := *targetURL
		upstream.Path = r.URL.Path
		upstream.RawQuery = r.URL.RawQuery

		// Create upstream request
		upReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstream.String(), r.Body)
		if err != nil {
			log.Printf("[%s] ERROR creating request: %v", r.Method, err)
			http.Error(w, `{"error":"proxy request creation failed"}`, http.StatusBadGateway)
			return
		}

		// Copy relevant headers from original request
		for _, h := range []string{"Content-Type", "Accept", "Authorization"} {
			if v := r.Header.Get(h); v != "" {
				upReq.Header.Set(h, v)
			}
		}

		// Set defaults
		if upReq.Header.Get("Content-Type") == "" {
			upReq.Header.Set("Content-Type", "application/json")
		}

		// Inject Cloudflare Zero Trust headers
		upReq.Header.Set("CF-Access-Client-Id", cfg.CFClientID)
		upReq.Header.Set("CF-Access-Client-Secret", cfg.CFClientSecret)
		upReq.Header.Set("Host", targetURL.Host)

		// Execute request
		resp, err := client.Do(upReq)
		if err != nil {
			log.Printf("[%s %s] ERROR upstream: %v (%s)", r.Method, r.URL.Path, err, time.Since(startTime))
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		// Copy response headers
		for key, vals := range resp.Header {
			lower := strings.ToLower(key)
			// Skip hop-by-hop headers
			if lower == "transfer-encoding" || lower == "connection" {
				continue
			}
			for _, v := range vals {
				w.Header().Add(key, v)
			}
		}

		// Detect if this is a streaming response (SSE)
		isStreaming := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

		if isStreaming {
			// SSE streaming: flush each chunk immediately
			w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering if present
			w.WriteHeader(resp.StatusCode)

			flusher, canFlush := w.(http.Flusher)

			buf := make([]byte, 4096)
			for {
				n, readErr := resp.Body.Read(buf)
				if n > 0 {
					if _, writeErr := w.Write(buf[:n]); writeErr != nil {
						log.Printf("[STREAM %s] client disconnected: %v", r.URL.Path, writeErr)
						return
					}
					if canFlush {
						flusher.Flush()
					}
				}
				if readErr != nil {
					if readErr != io.EOF {
						log.Printf("[STREAM %s] read error: %v", r.URL.Path, readErr)
					}
					break
				}
			}
			log.Printf("[STREAM %s %s] %d (%s)", r.Method, r.URL.Path, resp.StatusCode, time.Since(startTime))
		} else {
			// Non-streaming: copy entire response
			w.WriteHeader(resp.StatusCode)
			written, _ := io.Copy(w, resp.Body)
			log.Printf("[%s %s] %d %d bytes (%s)", r.Method, r.URL.Path, resp.StatusCode, written, time.Since(startTime))
		}
	})
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  export CF_ACCESS_CLIENT_ID='your-client-id'")
		fmt.Fprintln(os.Stderr, "  export CF_ACCESS_CLIENT_SECRET='your-client-secret'")
		fmt.Fprintln(os.Stderr, "  cf-llm-proxy [--port 8900] [--target llm.sark-ai.org]")
		os.Exit(1)
	}

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","target":"%s","version":"%s"}`, cfg.TargetHost, version)
	})

	// Proxy all other requests
	mux.Handle("/", proxyHandler(cfg))

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // No write timeout for streaming
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("cf-llm-proxy %s", version)
	log.Printf("Listening on http://%s", cfg.ListenAddr)
	log.Printf("Target: %s", cfg.TargetHost)
	log.Printf("CF-Access-Client-Id: %s...", cfg.CFClientID[:min(8, len(cfg.CFClientID))])
	log.Println("Ready.")

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
