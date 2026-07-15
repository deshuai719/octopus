package handlers

import (
	"errors"
	"net/http"

	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/upstreamintegration"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/integrations/upstream").
		AddRoute(
			router.NewRoute("/import/preview", http.MethodGet).
				Use(middleware.IntegrationAuth(op.IntegrationScopeImportRead)).
				Handle(upstreamImportPreview),
		).
		AddRoute(
			router.NewRoute("/import/resolve", http.MethodPost).
				Use(middleware.IntegrationAuth(op.IntegrationScopeImportResolve), middleware.RequireJSON()).
				Handle(upstreamImportResolve),
		).
		AddRoute(
			router.NewRoute("/auth/lease", http.MethodPost).
				Use(middleware.IntegrationAuth(op.IntegrationScopeAuthLease), middleware.RequireJSON()).
				Handle(upstreamAuthLease),
		).
		AddRoute(
			router.NewRoute("/ratio-change", http.MethodPost).
				Use(middleware.IntegrationAuth(op.IntegrationScopeRatioWrite), middleware.RequireJSON()).
				Handle(upstreamRatioChange),
		)
}

func upstreamImportPreview(c *gin.Context) {
	preview, err := upstreamintegration.BuildPreview(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, preview)
}

func upstreamImportResolve(c *gin.Context) {
	var request upstreamintegration.ResolveRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	resolved, err := upstreamintegration.Resolve(c.Request.Context(), request)
	if err != nil {
		respondUpstreamIntegrationError(c, err)
		return
	}
	resp.Success(c, resolved)
}

func upstreamAuthLease(c *gin.Context) {
	var request upstreamintegration.LeaseRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	lease, err := upstreamintegration.Lease(c.Request.Context(), request)
	if err != nil {
		respondUpstreamIntegrationError(c, err)
		return
	}
	resp.Success(c, lease)
}

func upstreamRatioChange(c *gin.Context) {
	var request siteRatioChangeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	matched, err := processSiteRatioChange(c.Request.Context(), request)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, gin.H{"matched_accounts": matched, "groups": request.Groups})
}

func respondUpstreamIntegrationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, upstreamintegration.ErrSiteNotFound), errors.Is(err, upstreamintegration.ErrAccountNotFound):
		resp.NotFound(c)
	case errors.Is(err, upstreamintegration.ErrUnsupportedPlatform), errors.Is(err, upstreamintegration.ErrCredentialUnavailable):
		resp.Error(c, http.StatusUnprocessableEntity, err.Error())
	default:
		resp.ErrorWithAppError(c, http.StatusBadGateway, err)
	}
}
