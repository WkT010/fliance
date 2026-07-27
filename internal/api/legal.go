package api

import (
	"embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed ../../web/legal/*.html
var legalFS embed.FS

type LegalHandler struct{}

func NewLegalHandler() *LegalHandler {
	return &LegalHandler{}
}

func (h *LegalHandler) Terms(c *gin.Context) {
	content, err := legalFS.ReadFile("web/legal/terms.html")
	if err != nil {
		c.String(http.StatusNotFound, "Page not found")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", content)
}

func (h *LegalHandler) Privacy(c *gin.Context) {
	content, err := legalFS.ReadFile("web/legal/privacy.html")
	if err != nil {
		c.String(http.StatusNotFound, "Page not found")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", content)
}

func (h *LegalHandler) Risks(c *gin.Context) {
	content, err := legalFS.ReadFile("web/legal/risks.html")
	if err != nil {
		c.String(http.StatusNotFound, "Page not found")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", content)
}

func (h *LegalHandler) AML(c *gin.Context) {
	content, err := legalFS.ReadFile("web/legal/aml.html")
	if err != nil {
		c.String(http.StatusNotFound, "Page not found")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", content)
}

func (h *LegalHandler) Cookies(c *gin.Context) {
	content, err := legalFS.ReadFile("web/legal/cookies.html")
	if err != nil {
		c.String(http.StatusNotFound, "Page not found")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", content)
}
