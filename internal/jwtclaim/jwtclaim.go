// Package jwtclaim parses unverified JWT claims for provider token metadata.
package jwtclaim

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/samber/oops"
)

// ParseUnverifiedClaims parses JWT claims without validating the token signature.
// Use it only for metadata from provider-issued tokens, never for authorization.
func ParseUnverifiedClaims(token string) (map[string]any, error) {
	claims := jwt.MapClaims{}

	_, _, err := jwt.NewParser().ParseUnverified(token, claims)
	if err != nil {
		return nil, oops.In("jwtclaim").Code("parse_unverified").Wrapf(err, "parse unverified jwt claims")
	}

	return claims, nil
}
