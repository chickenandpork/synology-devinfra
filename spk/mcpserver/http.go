package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type httpConfig struct {
	BindAddr string
	Path     string
	Token    string
}

type httpFileConfig struct {
	BindAddr string `json:"bind_addr"`
	Path     string `json:"path"`
	Token    string `json:"token"`
}

func runServeMode(ctx context.Context, args []string) error {
	cfg := defaultHTTPConfig()
	var configPath string
	fs := flag.NewFlagSet("mcpserver serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&configPath, "config", "", "HTTP config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if configPath != "" {
		if fileCfg, err := loadHTTPConfigFile(configPath); err != nil {
			return err
		} else {
			cfg = fileCfg
		}
	}
	return newServer(defaultConfig()).RunHTTP(ctx, cfg)
}

func defaultHTTPConfig() httpConfig {
	return httpConfig{
		BindAddr: envOrDefault("MCP_SYNO_HTTP_BIND", "0.0.0.0:8787"),
		Path:     envOrDefault("MCP_SYNO_HTTP_PATH", "/mcp"),
		Token:    envOrDefault("MCP_SYNO_HTTP_TOKEN", ""),
	}
}

func loadHTTPConfigFile(path string) (httpConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultHTTPConfig(), nil
		}
		return httpConfig{}, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return defaultHTTPConfig(), nil
	}
	var fileCfg httpFileConfig
	if err := json.Unmarshal(raw, &fileCfg); err != nil {
		return httpConfig{}, err
	}
	cfg := defaultHTTPConfig()
	if fileCfg.BindAddr != "" {
		cfg.BindAddr = fileCfg.BindAddr
	}
	if fileCfg.Path != "" {
		cfg.Path = fileCfg.Path
	}
	if fileCfg.Token != "" {
		cfg.Token = fileCfg.Token
	}
	return cfg, nil
}

func (s *server) RunHTTP(ctx context.Context, cfg httpConfig) error {
	mux := s.httpMux(cfg)

	srv := &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	port, err := bindPort(cfg.BindAddr)
	if err != nil {
		return err
	}

	listeners, err := listenHTTP(port)
	if err != nil {
		return err
	}
	defer func() {
		for _, ln := range listeners {
			_ = ln.Close()
		}
	}()

	errCh := make(chan error, len(listeners))
	for _, ln := range listeners {
		go func(ln net.Listener) {
			errCh <- srv.Serve(ln)
		}(ln)
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *server) httpMux(cfg httpConfig) http.Handler {
	mux := http.NewServeMux()
	path := normalizeHTTPPath(cfg.Path)
	mux.HandleFunc(path, s.observedHTTPHandler(path, true, s.httpHandler(cfg)))
	mux.HandleFunc(path+"/", s.observedHTTPHandler(path, true, s.httpHandler(cfg)))
	mux.HandleFunc("/spk-upload", s.observedHTTPHandler("/spk-upload", true, s.uploadSPKHandler()))
	mux.HandleFunc("/spk-upload/", s.observedHTTPHandler("/spk-upload", true, s.uploadSPKHandler()))
	mux.HandleFunc("/metrics", s.observedHTTPHandler("/metrics", false, s.metricsHandler()))
	mux.HandleFunc("/metrics/", s.observedHTTPHandler("/metrics", false, s.metricsHandler()))
	return mux
}

func listenHTTP(port string) ([]net.Listener, error) {
	v4, err := net.Listen("tcp4", net.JoinHostPort("0.0.0.0", port))
	if err != nil {
		return nil, err
	}

	v6, err := net.Listen("tcp6", net.JoinHostPort("::", port))
	if err != nil {
		_ = v4.Close()
		return nil, err
	}

	return []net.Listener{v4, v6}, nil
}

func bindPort(bindAddr string) (string, error) {
	_, port, err := net.SplitHostPort(bindAddr)
	if err != nil {
		return "", err
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", err
	}
	return port, nil
}

func (s *server) observedHTTPHandler(route string, countActivity bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		if s.metrics != nil {
			s.metrics.beginHTTP()
			defer func() {
				s.metrics.endHTTP(route, r.Method, recorder.status, countActivity)
			}()
		}

		next(recorder, r)
	}
}

func (s *server) httpHandler(cfg httpConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !originAllowed(r) {
			http.Error(w, "invalid origin", http.StatusForbidden)
			return
		}

		if cfg.Token != "" && !bearerTokenMatches(r.Header.Get("Authorization"), cfg.Token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcpserver"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		switch r.Method {
		case http.MethodPost:
		case http.MethodGet:
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		default:
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if proto := r.Header.Get("MCP-Protocol-Version"); proto != "" && !supportsVersion(proto) {
			http.Error(w, "unsupported MCP-Protocol-Version", http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}

		var req rpcRequest
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&req); err != nil {
			http.Error(w, "invalid json-rpc request", http.StatusBadRequest)
			return
		}

		if len(req.ID) == 0 || string(req.ID) == "null" {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		resp := s.handle(r.Context(), req)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (s *server) uploadSPKHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut:
		default:
			w.Header().Set("Allow", "POST, PUT")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path, checksum, size, err := writeUploadedSPK(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(spkUploadResult{
			Action:         "upload_spk",
			Source:         "stream",
			TempFile:       path,
			ChecksumSHA256: checksum,
			SizeBytes:      size,
		}); err != nil {
			http.Error(w, "failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}

func (s *server) metricsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
		case http.MethodHead:
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			return
		default:
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if s.metrics != nil {
			_ = s.metrics.render(w)
		}
	}
}

func normalizeHTTPPath(path string) string {
	if path == "" {
		return "/mcp"
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		return "/"
	}
	return trimmed
}

func originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func bearerTokenMatches(authHeader, want string) bool {
	if want == "" {
		return true
	}
	got := strings.TrimSpace(authHeader)
	if !strings.HasPrefix(strings.ToLower(got), "bearer ") {
		return false
	}
	return strings.TrimSpace(got[len("Bearer "):]) == want
}

func supportsVersion(version string) bool {
	for _, supported := range supportedVersions {
		if supported == version {
			return true
		}
	}
	return false
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(p)
}
