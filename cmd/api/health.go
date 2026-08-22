package main

import (
	"log/slog"
	"net/http"
)

func healthHandler(logger *slog.Logger) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		response := healthResponse{
			Status: "ok",
		}
		err := writeJSON(w, http.StatusOK, response)

		if err != nil {
			logger.Error("failed to encode response", "error", err)
		}

	}

}

type healthResponse struct {
	Status string `json:"status"`
}
