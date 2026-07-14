package middleware

import (
	"strings"

	"github.com/bestruirui/octopus/internal/requestmeta"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func OperationID() gin.HandlerFunc {
	return func(c *gin.Context) {
		operationID := normalizeOperationID(c.GetHeader(requestmeta.OperationIDHeader))
		if operationID == "" {
			operationID = uuid.NewString()
		}
		c.Set(requestmeta.OperationIDKey, operationID)
		c.Request = c.Request.WithContext(requestmeta.WithOperationID(c.Request.Context(), operationID))
		c.Header(requestmeta.OperationIDHeader, operationID)
		c.Next()
	}
}

func normalizeOperationID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 36 {
		return ""
	}
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		return ""
	}
	return value
}
