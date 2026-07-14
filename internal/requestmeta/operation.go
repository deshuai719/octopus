package requestmeta

import (
	"context"

	"github.com/gin-gonic/gin"
)

const (
	OperationIDHeader = "X-Octopus-Operation-ID"
	OperationIDKey    = "octopus_operation_id"
)

type operationIDContextKey struct{}

func WithOperationID(ctx context.Context, operationID string) context.Context {
	return context.WithValue(ctx, operationIDContextKey{}, operationID)
}

func OperationID(ctx context.Context) string {
	value, _ := ctx.Value(operationIDContextKey{}).(string)
	return value
}

func GinOperationID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	value, _ := c.Get(OperationIDKey)
	operationID, _ := value.(string)
	return operationID
}
