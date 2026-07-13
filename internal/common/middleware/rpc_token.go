package middleware

import (
	"crypto/subtle"
	"fmt"
	"os"

	"github.com/aqi/qlink-server/internal/common/response"
	"github.com/gin-gonic/gin"
)

const defaultRPCToken = "rpc-token-default"

// RpcTokenMiddleware validates the rpc-token header for inter-service calls.
func RpcTokenMiddleware(expectedToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("rpc-token")
		// Use constant-time comparison to prevent timing attacks.
		// Previously used != which short-circuits on first mismatch, allowing
		// attackers to brute-force the token character by character.
		if subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
			c.AbortWithStatusJSON(200, response.BuildError("invalid rpc-token"))
			return
		}
		c.Next()
	}
}

// ValidateRPCToken checks if RPC_TOKEN is set to a secure value.
// Returns an error if it is empty or still set to the default value.
func ValidateRPCToken() error {
	token := os.Getenv("RPC_TOKEN")
	if token == "" || token == defaultRPCToken {
		return fmt.Errorf("WARNING: RPC_TOKEN is not configured or still using default value 'rpc-token-default'. Set a secure RPC_TOKEN environment variable")
	}
	return nil
}
