package resp

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/requestmeta"
	"github.com/gin-gonic/gin"
)

type ResponseStruct struct {
	Code            int            `json:"code" example:"200"`
	ErrorCode       string         `json:"error_code,omitempty" example:"site.sub2api.api_key_required"`
	Message         string         `json:"message" example:"success"`
	Params          map[string]any `json:"params,omitempty"`
	Data            interface{}    `json:"data,omitempty"`
	OperationID     string         `json:"operation_id,omitempty"`
	Stage           string         `json:"stage,omitempty"`
	Retryable       *bool          `json:"retryable,omitempty"`
	SuggestedAction string         `json:"suggested_action,omitempty"`
}

func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, ResponseStruct{
		Code:        http.StatusOK,
		Message:     "success",
		Data:        data,
		OperationID: requestmeta.GinOperationID(c),
	})
}

func Error(c *gin.Context, code int, err string) {
	ErrorWithCode(c, code, "", err)
}

func ErrorWithAppError(c *gin.Context, fallbackStatus int, err error) {
	status := fallbackStatus
	if appStatus := apperror.Status(err); appStatus != 0 {
		status = appStatus
	}
	ErrorWithDetails(c, status, apperror.Code(err), apperror.Message(err), apperror.Params(err), apperror.Stage(err), apperror.Retryable(err), apperror.SuggestedAction(err))
}

func ErrorWithCode(c *gin.Context, status int, errorCode string, message string) {
	ErrorWithCodeAndParams(c, status, errorCode, message, nil)
}

func ErrorWithCodeAndParams(c *gin.Context, status int, errorCode string, message string, params map[string]any) {
	ErrorWithDetails(c, status, errorCode, message, params, "", nil, "")
}

func ErrorWithDetails(c *gin.Context, status int, errorCode string, message string, params map[string]any, stage string, retryable *bool, suggestedAction string) {
	c.AbortWithStatusJSON(status, ResponseStruct{
		Code:            status,
		ErrorCode:       errorCode,
		Message:         message,
		Params:          params,
		OperationID:     requestmeta.GinOperationID(c),
		Stage:           stage,
		Retryable:       retryable,
		SuggestedAction: suggestedAction,
	})
}

func InvalidJSON(c *gin.Context) {
	ErrorWithAppError(c, http.StatusBadRequest, apperror.InvalidJSON(ErrInvalidJSON))
}

func InvalidParam(c *gin.Context) {
	ErrorWithAppError(c, http.StatusBadRequest, apperror.InvalidParam(ErrInvalidParam))
}

func InternalError(c *gin.Context) {
	ErrorWithAppError(c, http.StatusInternalServerError, apperror.New(apperror.CodeCommonInternalError, ErrInternalServer).WithStatus(http.StatusInternalServerError))
}

func DatabaseError(c *gin.Context) {
	ErrorWithAppError(c, http.StatusInternalServerError, apperror.New(apperror.CodeCommonDatabaseError, ErrDatabase).WithStatus(http.StatusInternalServerError))
}

func NotFound(c *gin.Context) {
	ErrorWithAppError(c, http.StatusNotFound, apperror.New(apperror.CodeCommonNotFound, ErrResourceNotFound).WithStatus(http.StatusNotFound))
}

func DuplicateResource(c *gin.Context) {
	ErrorWithAppError(c, http.StatusConflict, apperror.New(apperror.CodeCommonDuplicateResource, ErrDuplicateResource).WithStatus(http.StatusConflict))
}

func Unauthorized(c *gin.Context) {
	ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthUnauthorized, ErrUnauthorized).WithStatus(http.StatusUnauthorized))
}

func InvalidToken(c *gin.Context) {
	ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthInvalidToken, ErrUnauthorized).WithStatus(http.StatusUnauthorized))
}

func InvalidCredentials(c *gin.Context) {
	ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthInvalidCredentials, ErrUnauthorized).WithStatus(http.StatusUnauthorized))
}

func APIKeyMissing(c *gin.Context) {
	ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthAPIKeyMissing, "API key is missing").WithStatus(http.StatusUnauthorized))
}

func APIKeyExpired(c *gin.Context) {
	ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthAPIKeyExpired, "API key has expired").WithStatus(http.StatusUnauthorized))
}
