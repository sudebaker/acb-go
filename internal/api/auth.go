package api

import (
	"net/http"
	"strings"

	"github.com/sudebaker/acb-go/internal/db"
)

func AuthMiddleware(repo *db.AgentRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" || r.URL.Path == "/health/" {
				next.ServeHTTP(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				WriteError(w, 401, "unauthorized", "missing or invalid authorization header")
				return
			}

			token := strings.TrimPrefix(auth, "Bearer ")
			agent, err := repo.GetByToken(token)
			if err != nil || agent == nil {
				WriteError(w, 401, "unauthorized", "invalid token")
				return
			}

			r.Header.Set("X-Agent-Name", agent.Name)
			next.ServeHTTP(w, r)
		})
	}
}
