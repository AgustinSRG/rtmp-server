// Websocket authentication

package main

import (
	"os"

	"github.com/golang-jwt/jwt/v5"
)

// Creates an authentication token to connect
// to the coordinator server
// Returns the token (base 64)
func MakeWebsocketAuthenticationToken() string {
	secret := os.Getenv("CONTROL_SECRET")

	if secret == "" {
		return ""
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "rtmp-control",
	})

	tokenBase64, e := token.SignedString([]byte(secret))

	if e != nil {
		LogError(e)
		return ""
	}

	return tokenBase64
}
