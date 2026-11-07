// Copyright 2026 The go-runtime Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package server runs the http services of a kernel with net/http: TLS,
// timeouts and a graceful shutdown that lets running requests end with
// the generation they started with.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Config configures a server. Zero durations take the defaults.
type Config struct {
	Addr            string // e.g. :8080
	TLSCert, TLSKey string // both or neither
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

// Defaults of Config.
const (
	DefaultAddr            = ":8080"
	DefaultReadTimeout     = 30 * time.Second
	DefaultWriteTimeout    = 60 * time.Second
	DefaultShutdownTimeout = 30 * time.Second
	readHeaderTimeout      = 10 * time.Second
)

func (c Config) withDefaults() Config {
	if c.Addr == "" {
		c.Addr = DefaultAddr
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = DefaultReadTimeout
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = DefaultWriteTimeout
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = DefaultShutdownTimeout
	}
	return c
}

// Run listens on the address of the configuration and serves until the
// context ends; see Serve.
func Run(ctx context.Context, cfg Config, h http.Handler) error {
	cfg = cfg.withDefaults()
	if (cfg.TLSCert == "") != (cfg.TLSKey == "") {
		return errors.New("server: a TLS certificate needs a key and a key a certificate")
	}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return Serve(ctx, ln, cfg, h)
}

// Serve serves on a listener until the context ends, then shuts down: it
// stops accepting connections and waits for running requests at most the
// shutdown timeout before it closes the remaining connections.
func Serve(ctx context.Context, ln net.Listener, cfg Config, h http.Handler) error {
	cfg = cfg.withDefaults()
	srv := &http.Server{
		Handler:           h,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: min(readHeaderTimeout, cfg.ReadTimeout),
		WriteTimeout:      cfg.WriteTimeout,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}
	secure := cfg.TLSCert != ""
	if secure {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			ln.Close()
			return fmt.Errorf("server: %w", err)
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		ln = tls.NewListener(ln, srv.TLSConfig)
	}
	slog.Info("serving http services", "addr", ln.Addr().String(), "tls", secure)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
	}
	slog.Info("shutting down", "timeout", cfg.ShutdownTimeout)
	sctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	err := srv.Shutdown(sctx)
	if errors.Is(err, context.DeadlineExceeded) {
		slog.Warn("requests still ran at the end of the shutdown timeout; closing their connections")
		err = srv.Close()
	}
	if serr := <-done; !errors.Is(serr, http.ErrServerClosed) && err == nil {
		err = serr
	}
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}
	slog.Info("server stopped")
	return nil
}
