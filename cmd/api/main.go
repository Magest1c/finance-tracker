package main

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func newApplication(cfg config, logOutput io.Writer) *application {
	if logOutput == nil {
		logOutput = io.Discard
	}

	return &application{
		config:       cfg,
		logger:       slog.New(slog.NewJSONHandler(logOutput, nil)),
		users:        newUserStore(),
		tokens:       newTokenStore(),
		accounts:     newAccountStore(),
		categories:   newCategoryStore(),
		transactions: newTransactionStore(),
	}
}

func main() {
	cfg := loadConfig()
	app := newApplication(cfg, os.Stdout)
	server := &http.Server{
		Addr:              app.config.httpAddr,
		Handler:           app.router(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	app.logger.Info("starting server", "addr", server.Addr)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		app.logger.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
}
