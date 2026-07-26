package api

import (
	"errors"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

type APIError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

func (e *APIError) Error() string { return e.Message }

var (
	ErrBadRequest   = &APIError{400, "bad request", nil}
	ErrUnauthorized = &APIError{401, "unauthorized", nil}
	ErrForbidden    = &APIError{403, "forbidden", nil}
	ErrNotFound     = &APIError{404, "not found", nil}
	ErrRateLimited  = &APIError{429, "rate limit exceeded", nil}
	ErrInternal     = &APIError{500, "internal server error", nil}
	ErrEngineBusy   = &APIError{503, "matching engine busy", nil}
	ErrInvalidOrder = &APIError{400, "invalid order parameters", nil}
)

func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				gin.DefaultWriter.Write([]byte("[PANIC] " + string(debug.Stack()) + "\n"))
				c.AbortWithStatusJSON(500, gin.H{"error": "internal error", "code": 500})
			}
		}()
		c.Next()
		if len(c.Errors) > 0 {
			e := c.Errors.Last().Err
			var ae *APIError
			if errors.As(e, &ae) {
				c.JSON(ae.Code, gin.H{"error": ae.Message, "code": ae.Code})
			}
		}
	}
}
