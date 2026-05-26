package api

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"
)

func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorResponse{Error: code, Message: message})
}

// WriteErrorSafe logs the error and returns a generic message to the client.
func WriteErrorSafe(w http.ResponseWriter, status int, code string, err error) {
	if err != nil {
		log.Error().Err(err).Str("code", code).Msg("internal error")
	}
	WriteError(w, status, code, "internal server error")
}

type ConflictResponse struct {
	Error         string `json:"error"`
	Message       string `json:"message,omitempty"`
	CurrentStatus string `json:"current_status"`
}

func WriteConflict(w http.ResponseWriter, code, message, currentStatus string) {
	WriteJSON(w, 409, ConflictResponse{Error: code, Message: message, CurrentStatus: currentStatus})
}
