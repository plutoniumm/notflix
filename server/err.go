package server

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

func Error(message string, c *gin.Context, statusCode int) {
	fmt.Println("Error:", message)
	c.String(statusCode, message)
}
