package httpapi

import (
	"encoding/json"
	"errors"
	nethttp "net/http"
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

type dataResponse struct {
	Data interface{} `json:"data"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

func writeData(w nethttp.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(dataResponse{Data: data})
}

func writeError(w nethttp.ResponseWriter, status int, code string, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: errorDetail{Code: code, Message: msg}})
}

func serviceErrorToHTTP(err error) (int, string) {
	if errors.Is(err, core.ErrNotFound) {
		return nethttp.StatusNotFound, "not_found"
	}
	if strings.Contains(strings.ToLower(err.Error()), "not found") {
		return nethttp.StatusNotFound, "not_found"
	}
	if errors.Is(err, core.ErrInvalidInput) {
		return nethttp.StatusBadRequest, "validation_error"
	}
	if strings.Contains(strings.ToLower(err.Error()), "invalid input") {
		return nethttp.StatusBadRequest, "validation_error"
	}
	return nethttp.StatusInternalServerError, "internal_error"
}
