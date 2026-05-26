package api

import (
	"database/sql"
	"net/http"

	goredis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/sudebaker/acb-go/internal/rustfs"
)

type componentStatus struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type healthResponse struct {
	Status string                     `json:"status"`
	Checks map[string]componentStatus `json:"checks"`
}

type HealthHandler struct {
	db          *sql.DB
	rdb         *goredis.Client
	rustfs      *rustfs.Client
}

func NewHealthHandler(db *sql.DB, rdb *goredis.Client, rustfsClient *rustfs.Client) *HealthHandler {
	return &HealthHandler{db: db, rdb: rdb, rustfs: rustfsClient}
}

func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp := healthResponse{
		Status: "ok",
		Checks: make(map[string]componentStatus),
	}

	// Database check
	if h.db != nil {
		if err := h.db.PingContext(ctx); err != nil {
			resp.Checks["database"] = componentStatus{Status: "error", Error: err.Error()}
			resp.Status = "degraded"
			log.Error().Err(err).Msg("health: database check failed")
		} else {
			resp.Checks["database"] = componentStatus{Status: "ok"}
		}
	} else {
		resp.Checks["database"] = componentStatus{Status: "disabled"}
	}

	// Redis check
	if h.rdb != nil {
		if err := h.rdb.Ping(ctx).Err(); err != nil {
			resp.Checks["redis"] = componentStatus{Status: "error", Error: err.Error()}
			resp.Status = "degraded"
			log.Error().Err(err).Msg("health: redis check failed")
		} else {
			resp.Checks["redis"] = componentStatus{Status: "ok"}
		}
	} else {
		resp.Checks["redis"] = componentStatus{Status: "disabled"}
	}

	// RustFS check
	if h.rustfs != nil && h.rustfs.Enabled() {
		if err := h.rustfs.EnsureBucket(ctx); err != nil {
			resp.Checks["storage"] = componentStatus{Status: "error", Error: err.Error()}
			resp.Status = "degraded"
			log.Error().Err(err).Msg("health: storage check failed")
		} else {
			resp.Checks["storage"] = componentStatus{Status: "ok"}
		}
	} else {
		resp.Checks["storage"] = componentStatus{Status: "disabled"}
	}

	WriteJSON(w, 200, resp)
}
