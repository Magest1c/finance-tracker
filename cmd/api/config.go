package main

import (
	"os"
	"strings"
)

type config struct {
	httpAddr string
}

func loadConfig() config {
	httpAddr := strings.TrimSpace(os.Getenv("HTTP_ADDR"))

	if httpAddr == "" {
		httpAddr = ":8080"
	}

	return config{
		httpAddr: httpAddr,
	}
}
