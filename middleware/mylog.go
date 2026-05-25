package middleware

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/gin-gonic/gin"
)

const maxLoggedBodyBytes = 8 << 10 // 8 KiB

func MyLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		query := c.Request.URL.RawQuery
		body := readAndRestoreBody(c)

		logger.LogInfo(c.Request.Context(), fmt.Sprintf(
			"request received method=%s path=%q client_ip=%s query=%q body=%q",
			c.Request.Method, c.Request.URL.Path, c.ClientIP(), query, body,
		))

		c.Next()
	}
}

func readAndRestoreBody(c *gin.Context) string {
	if c.Request.Body == nil || c.Request.Method == http.MethodGet {
		return ""
	}
	if !shouldLogBody(c.GetHeader("Content-Type")) {
		return "<omitted>"
	}

	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return fmt.Sprintf("<read error: %s>", err.Error())
	}
	_ = c.Request.Body.Close()
	c.Request.Body = io.NopCloser(bytes.NewBuffer(data))

	//if len(data) > maxLoggedBodyBytes {
	//	return string(data[:maxLoggedBodyBytes]) + "...<truncated>"
	//}
	return string(data)
}

func shouldLogBody(contentType string) bool {
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "application/json"),
		strings.Contains(ct, "application/x-www-form-urlencoded"),
		strings.Contains(ct, "text/"):
		return true
	}
	return false
}
