package auth

import "strings"

// timestampMillisThreshold separates second epoch timestamps from millisecond epoch timestamps.
const timestampMillisThreshold int64 = 100_000_000_000

func (credential *Credential) apiKeyValue() (string, bool) {
	switch credential.Type {
	case CredentialTypeAPIKey:
		resolved := resolveStoredKey(credential.Key)
		return resolved, resolved != ""
	case CredentialTypeOAuth:
		access := credential.oauthAccess()
		return access, access != "" && !credential.oauthExpired()
	default:
		return "", false
	}
}

func (credential *Credential) hasSecretMaterial() bool {
	switch credential.Type {
	case CredentialTypeAPIKey:
		return strings.TrimSpace(credential.Key) != ""
	case CredentialTypeOAuth:
		return credential.oauthAccess() != "" || credential.oauthRefresh() != ""
	default:
		return false
	}
}

func (credential *Credential) oauthAccess() string {
	if credential.Access != "" {
		return credential.Access
	}
	if credential.OAuth != nil {
		return credential.OAuth["access"]
	}

	return ""
}

func (credential *Credential) oauthRefresh() string {
	if credential.Refresh != "" {
		return credential.Refresh
	}
	if credential.OAuth != nil {
		return credential.OAuth["refresh"]
	}

	return ""
}

func (credential *Credential) oauthExpired() bool {
	expires := credential.Expires
	if expires == 0 {
		expires = credential.ExpiresAt
	}
	if expires == 0 || expires > timestampMillisThreshold {
		return expires != 0 && timeNowMillis() >= expires
	}

	return timeNowMillis() >= expires*1000
}
