// Package jwtclaim parses verified JWT claims for provider token metadata.
package jwtclaim

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/samber/oops"
)

// ParseClaims parses JWT claims after validating the token signature.
func ParseClaims(token string, keyFunc jwt.Keyfunc) (map[string]any, error) {
	claims := jwt.MapClaims{}

	parsed, err := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
	).ParseWithClaims(token, claims, keyFunc)
	if err != nil {
		return nil, oops.In("jwtclaim").Code("parse").Wrapf(err, "parse jwt claims")
	}

	if !parsed.Valid {
		return nil, oops.In("jwtclaim").Code("invalid").Errorf("jwt is invalid")
	}

	return claims, nil
}
