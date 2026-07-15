package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/gin-gonic/gin"
)

const integrationTokenContextKey = "integration_token_id"

// IntegrationAuth authenticates the dedicated service token namespace. It is
// intentionally separate from administrator JWT and public API key auth.
func IntegrationAuth(requiredScopes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := strings.TrimSpace(c.GetHeader("Authorization"))
		if !strings.HasPrefix(raw, "Bearer ") {
			resp.Unauthorized(c)
			c.Abort()
			return
		}
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))
		token, err := op.IntegrationTokenAuthenticate(raw, requiredScopes, c.Request.Context())
		if err != nil {
			switch {
			case errors.Is(err, op.ErrIntegrationTokenInvalid):
				resp.InvalidToken(c)
			case errors.Is(err, op.ErrIntegrationTokenForbidden):
				resp.ErrorWithAppError(c, http.StatusForbidden, apperror.New(apperror.CodeAuthForbidden, "integration token scope is missing").WithStatus(http.StatusForbidden))
			default:
				resp.InternalError(c)
			}
			c.Abort()
			return
		}
		c.Set(integrationTokenContextKey, token.ID)
		c.Next()
	}
}
