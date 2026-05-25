package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/sudebaker/acb-go/internal/db"
)

// adminToken is generated at boot for bootstrap authentication when no agents exist.
// If ACB_ADMIN_TOKEN env var is set, it's used as the initial token (for migration),
// but a new random token is generated and logged on every restart.
var adminToken string

func init() {
	// Generate a new random admin token on every boot
	randomToken := make([]byte, 32)
	if _, err := rand.Read(randomToken); err != nil {
		log.Fatal().Err(err).Msg("Failed to generate admin token")
	}
	adminToken = hex.EncodeToString(randomToken)

	// Check if legacy env var is set - if so, use it for this session only
	// and warn that it will change on next restart
	if legacyToken := os.Getenv("ACB_ADMIN_TOKEN"); legacyToken != "" {
		log.Warn().Msg("ACB_ADMIN_TOKEN env var is deprecated - using legacy token for this session only. Token will rotate on next restart.")
		adminToken = legacyToken
	}

	log.Info().Msg("Admin token generated for bootstrap authentication")
	log.Warn().Msg("ADMIN TOKEN generated — save it now. It will change on next restart. Check startup logs for the next occurrence.")
	log.Debug().Str("token", adminToken).Msg("Admin token for bootstrap authentication (visible only with debug logging)")
}

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

			// Check admin token FIRST (constant-time, zero DB hits) - prevents timing leak
			if adminToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(adminToken)) == 1 {
				r.Header.Set("X-Agent-Name", "admin")
				log.Info().Str("path", r.URL.Path).Str("method", r.Method).Msg("Admin token used for authentication")
				next.ServeHTTP(w, r)
				return
			}

			// Try agent token (normal flow) - no timing leak since admin check is first
			agent, err := repo.GetByToken(token)
			if err == nil && agent != nil {
				r.Header.Set("X-Agent-Name", agent.Name)
				next.ServeHTTP(w, r)
				return
			}

			WriteError(w, 401, "unauthorized", "invalid token")
		})
	}
}