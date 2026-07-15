package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

type createIntegrationTokenRequest struct {
	Name   string   `json:"name" binding:"required"`
	Scopes []string `json:"scopes" binding:"required"`
}

func init() {
	router.NewGroupRouter("/api/v1/integrations/tokens").
		Use(middleware.Auth()).
		AddRoute(router.NewRoute("", http.MethodGet).Handle(listIntegrationTokens)).
		AddRoute(router.NewRoute("", http.MethodPost).Use(middleware.RequireJSON()).Handle(createIntegrationToken)).
		AddRoute(router.NewRoute("/:id/revoke", http.MethodPost).Handle(revokeIntegrationToken)).
		AddRoute(router.NewRoute("/:id/rotate", http.MethodPost).Handle(rotateIntegrationToken))
}

func createIntegrationToken(c *gin.Context) {
	var req createIntegrationTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	created, err := op.IntegrationTokenCreate(req.Name, req.Scopes, c.Request.Context())
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	resp.Success(c, created)
}

func listIntegrationTokens(c *gin.Context) {
	tokens, err := op.IntegrationTokenList(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, tokens)
}

func revokeIntegrationToken(c *gin.Context) {
	id, ok := integrationTokenID(c)
	if !ok {
		return
	}
	if err := op.IntegrationTokenRevoke(id, c.Request.Context()); err != nil {
		if errors.Is(err, op.ErrIntegrationTokenNotFound) {
			resp.NotFound(c)
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func rotateIntegrationToken(c *gin.Context) {
	id, ok := integrationTokenID(c)
	if !ok {
		return
	}
	created, err := op.IntegrationTokenRotate(id, c.Request.Context())
	if err != nil {
		if errors.Is(err, op.ErrIntegrationTokenNotFound) {
			resp.NotFound(c)
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, created)
}

func integrationTokenID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		resp.InvalidParam(c)
		return 0, false
	}
	return id, true
}
