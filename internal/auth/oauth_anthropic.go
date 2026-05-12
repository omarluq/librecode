package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/limitio"
)

const (
	anthropicClaudeProvider = "anthropic-claude"
	anthropicClientID       = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	anthropicAuthorize      = "https://claude.ai/oauth/authorize"
)

const anthropicScope = "org:create_api_key user:profile user:inference " +
	"user:sessions:claude_code user:mcp_servers user:file_upload"

var (
	anthropicRedirectURI = anthropicRedirectEndpoint()
	anthropicTokenURL    = anthropicTokenEndpoint()
)

func anthropicRedirectEndpoint() string {
	return "http://localhost:53692/callback"
}

func anthropicTokenEndpoint() string {
	return "https://platform.claude.com/v1/oauth/token"
}

// AnthropicLoginURL creates the Claude Pro/Max OAuth URL.
func AnthropicLoginURL() (string, error) {
	flow, err := newAnthropicFlow()
	if err != nil {
		return "", err
	}

	return flow.URL, nil
}

// LoginAnthropic starts the Claude Pro/Max OAuth browser flow.
func LoginAnthropic(_ context.Context, onAuth func(OAuthAuthInfo)) (*Credential, error) {
	flowURL, err := AnthropicLoginURL()
	if err != nil {
		return nil, err
	}
	if onAuth != nil {
		onAuth(OAuthAuthInfo{
			URL: flowURL,
			Instructions: "Complete Anthropic login in your browser, then paste the authorization code " +
				"shown by Claude.",
		})
	}

	return nil, oops.In("auth").
		Code("anthropic_code_required").
		Errorf("anthropic oauth requires pasted authorization code")
}

// LoginAnthropicWithCode completes Claude Pro/Max OAuth using the pasted code from Claude.
func LoginAnthropicWithCode(ctx context.Context, authCode string) (*Credential, error) {
	code, state, ok := strings.Cut(strings.TrimSpace(authCode), "#")
	if !ok || code == "" || state == "" {
		return nil, oops.In("auth").
			Code("anthropic_code_format").
			Errorf("paste the full Anthropic authorization code in the form code#state")
	}

	credential, err := exchangeAnthropicCode(ctx, code, state, state)
	if err != nil {
		return nil, err
	}

	return credential, nil
}

func anthropicAPIKey(ctx context.Context, credential *Credential) (*Credential, string, error) {
	if credential == nil || credential.Type != CredentialTypeOAuth {
		return credential, "", nil
	}
	if access := credential.oauthAccess(); access != "" && !credential.oauthExpired() {
		return credential, access, nil
	}
	refresh := credential.oauthRefresh()
	if refresh == "" {
		return credential, "", nil
	}
	refreshed, err := refreshAnthropic(ctx, refresh)
	if err != nil {
		return credential, "", err
	}
	access := refreshed.oauthAccess()

	return refreshed, access, nil
}

func refreshAnthropic(ctx context.Context, refreshToken string) (*Credential, error) {
	return requestAnthropicToken(ctx, map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     anthropicClientID,
		"refresh_token": refreshToken,
	})
}

type anthropicFlow struct {
	Verifier string
	State    string
	URL      string
}

func newAnthropicFlow() (*anthropicFlow, error) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, err
	}
	authURL, err := url.Parse(anthropicAuthorize)
	if err != nil {
		return nil, oops.In("auth").Code("anthropic_authorize_url").Wrapf(err, "parse authorize url")
	}
	query := authURL.Query()
	query.Set("code", "true")
	query.Set("client_id", anthropicClientID)
	query.Set("response_type", "code")
	query.Set("redirect_uri", anthropicRedirectURI)
	query.Set("scope", anthropicScope)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("state", verifier)
	authURL.RawQuery = query.Encode()

	return &anthropicFlow{Verifier: verifier, State: verifier, URL: authURL.String()}, nil
}

func exchangeAnthropicCode(ctx context.Context, code, state, verifier string) (*Credential, error) {
	return requestAnthropicToken(ctx, map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     anthropicClientID,
		"code":          code,
		"state":         state,
		"redirect_uri":  anthropicRedirectURI,
		"code_verifier": verifier,
	})
}

func requestAnthropicToken(ctx context.Context, payload map[string]string) (*Credential, error) {
	body, err := postAnthropicToken(ctx, payload)
	if err != nil {
		return nil, err
	}

	return decodeAnthropicToken(body)
}

func postAnthropicToken(ctx context.Context, payload map[string]string) ([]byte, error) {
	content, err := json.Marshal(payload)
	if err != nil {
		return nil, oops.In("auth").Code("anthropic_token_payload").Wrapf(err, "encode token payload")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicTokenURL, bytes.NewReader(content))
	if err != nil {
		return nil, oops.In("auth").Code("anthropic_token_request").Wrapf(err, "create token request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, oops.In("auth").Code("anthropic_token_http").Wrapf(err, "request token")
	}
	defer closeAuthBody(response.Body)
	body, err := limitio.ReadAll(response.Body, 1<<20, "anthropic token response")
	if err != nil {
		return nil, oops.In("auth").Code("anthropic_token_body").Wrapf(err, "read token response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, oops.In("auth").
			Code("anthropic_token_status").
			With("status", response.StatusCode).
			Errorf("token request failed: %s", strings.TrimSpace(string(body)))
	}

	return body, nil
}

func decodeAnthropicToken(body []byte) (*Credential, error) {
	var tokenData struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenData); err != nil {
		return nil, oops.In("auth").Code("anthropic_token_decode").Wrapf(err, "decode token response")
	}
	if tokenData.AccessToken == "" || tokenData.RefreshToken == "" || tokenData.ExpiresIn <= 0 {
		return nil, oops.In("auth").Code("anthropic_token_invalid").Errorf("token response is incomplete")
	}

	return &Credential{
		OAuth:     nil,
		Type:      CredentialTypeOAuth,
		Key:       "",
		Access:    tokenData.AccessToken,
		Refresh:   tokenData.RefreshToken,
		AccountID: "",
		Expires:   time.Now().Add(time.Duration(tokenData.ExpiresIn)*time.Second - 5*time.Minute).UnixMilli(),
		ExpiresAt: 0,
	}, nil
}

func isAnthropicOAuthAccessToken(token string) bool {
	return strings.Contains(token, "sk-ant-oat")
}
