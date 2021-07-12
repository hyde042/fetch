package fetch

import (
	"errors"
	"fmt"
)

type OAuthToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

func (t OAuthToken) Header() (string, error) {
	switch t.TokenType {
	case "bearer":
		return "Bearer: " + t.AccessToken, nil
	case "":
		return "", errors.New("missing token type")
	default:
		return "", fmt.Errorf("unsupported token type: %s", t.TokenType)
	}
}
