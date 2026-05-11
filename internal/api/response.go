package api

import (
	"encoding/json"
	"net/http"
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

type ConflictResponse struct {
	Error         string `json:"error"`
	Message       string `json:"message,omitempty"`
	CurrentStatus string `json:"current_status"`
}

func WriteConflict(w http.ResponseWriter, code, message, currentStatus string) {
	WriteJSON(w, 409, ConflictResponse{Error: code, Message: message, CurrentStatus: currentStatus})
}
