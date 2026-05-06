package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/samber/oops"
)

const (
	openAICodexProvider    = "openai-codex"
	openAICodexClientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	openAICodexAuthorize   = "https://auth.openai.com/oauth/authorize"
	openAICodexExchangeURL = "https://auth.openai.com/oauth/" + "token"
	openAICodexRedirectURI = "http://localhost:1455/auth/callback"
	openAICodexScope       = "openid profile email offline_access"
	openAICodexJWTClaim    = "https://api.openai.com/auth"
	openAICodexCallback    = "127.0.0.1:1455"
)

// OAuthAuthInfo describes a browser OAuth step.
type OAuthAuthInfo struct {
	URL          string `json:"url"`
	Instructions string `json:"instructions,omitempty"`
}

// LoginOpenAICodex runs the ChatGPT/Codex OAuth browser flow.
func LoginOpenAICodex(ctx context.Context, onAuth func(OAuthAuthInfo)) (*Credential, error) {
	flow, err := newOpenAICodexFlow()
	if err != nil {
		return nil, err
	}
	server, err := startOpenAICodexCallbackServer(flow.State)
	if err != nil {
		return nil, err
	}
	defer server.Close(ctx)

	if onAuth != nil {
		onAuth(OAuthAuthInfo{
			URL:          flow.URL,
			Instructions: "Complete login in your browser to finish authentication.",
		})
	}

	code, err := server.Wait(ctx)
	if err != nil {
		return nil, err
	}
	credential, err := exchangeOpenAICodexCode(ctx, code, flow.Verifier)
	if err != nil {
		return nil, err
	}

	return credential, nil
}

func openAICodexAPIKey(ctx context.Context, credential *Credential) (*Credential, string, error) {
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
	refreshed, err := refreshOpenAICodex(ctx, refresh)
	if err != nil {
		return credential, "", err
	}
	access := refreshed.oauthAccess()

	return refreshed, access, nil
}

func refreshOpenAICodex(ctx context.Context, refreshToken string) (*Credential, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)
	values.Set("client_id", openAICodexClientID)

	return requestOpenAICodexToken(ctx, values)
}

type openAICodexFlow struct {
	Verifier string
	State    string
	URL      string
}

func newOpenAICodexFlow() (*openAICodexFlow, error) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, err
	}
	state, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	authURL, err := url.Parse(openAICodexAuthorize)
	if err != nil {
		return nil, oops.In("auth").Code("codex_authorize_url").Wrapf(err, "parse authorize url")
	}
	query := authURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", openAICodexClientID)
	query.Set("redirect_uri", openAICodexRedirectURI)
	query.Set("scope", openAICodexScope)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	query.Set("id_token_add_organizations", "true")
	query.Set("codex_cli_simplified_flow", "true")
	query.Set("originator", "pi")
	authURL.RawQuery = query.Encode()

	return &openAICodexFlow{Verifier: verifier, State: state, URL: authURL.String()}, nil
}

type openAICodexCallbackServer struct {
	server *http.Server
	codes  chan callbackResult
}

type callbackResult struct {
	Err  error
	Code string
}

func startOpenAICodexCallbackServer(state string) (*openAICodexCallbackServer, error) {
	codes := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("state") != state {
			writeOAuthHTML(writer, http.StatusBadRequest, "Authentication state mismatch.")
			codes <- callbackResult{Code: "", Err: fmt.Errorf("state mismatch")}
			return
		}
		code := query.Get("code")
		if code == "" {
			writeOAuthHTML(writer, http.StatusBadRequest, "Missing authorization code.")
			codes <- callbackResult{Code: "", Err: fmt.Errorf("missing authorization code")}
			return
		}
		writeOAuthHTML(writer, http.StatusOK, "librecode authentication complete. You can close this tab.")
		codes <- callbackResult{Code: code, Err: nil}
	})
	mux.HandleFunc("/", func(writer http.ResponseWriter, _ *http.Request) {
		writeOAuthHTML(writer, http.StatusNotFound, "Callback route not found.")
	})

	server := &http.Server{
		Addr:              openAICodexCallback,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", openAICodexCallback)
	if err != nil {
		return nil, oops.In("auth").Code("codex_callback_listen").Wrapf(err, "listen for oauth callback")
	}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			codes <- callbackResult{Code: "", Err: err}
		}
	}()

	return &openAICodexCallbackServer{server: server, codes: codes}, nil
}

func (server *openAICodexCallbackServer) Wait(ctx context.Context) (string, error) {
	select {
	case result := <-server.codes:
		if result.Err != nil {
			return "", oops.In("auth").Code("codex_callback").Wrapf(result.Err, "receive oauth callback")
		}

		return result.Code, nil
	case <-ctx.Done():
		return "", oops.In("auth").Code("codex_callback_canceled").Wrapf(ctx.Err(), "wait for oauth callback")
	}
}

func (server *openAICodexCallbackServer) Close(ctx context.Context) {
	shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := server.server.Shutdown(shutdownCtx); err != nil {
		return
	}
}

func writeOAuthHTML(writer http.ResponseWriter, status int, message string) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(status)
	content := fmt.Sprintf(
		"<!doctype html><title>librecode auth</title><main style='font-family:sans-serif'><h1>%s</h1></main>",
		html.EscapeString(message),
	)
	if _, err := writer.Write([]byte(content)); err != nil {
		return
	}
}

func closeAuthBody(body io.Closer) {
	if err := body.Close(); err != nil {
		return
	}
}

func exchangeOpenAICodexCode(ctx context.Context, code, verifier string) (*Credential, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("client_id", openAICodexClientID)
	values.Set("code", code)
	values.Set("code_verifier", verifier)
	values.Set("redirect_uri", openAICodexRedirectURI)

	return requestOpenAICodexToken(ctx, values)
}

func requestOpenAICodexToken(ctx context.Context, values url.Values) (*Credential, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		openAICodexExchangeURL,
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return nil, oops.In("auth").Code("codex_token_request").Wrapf(err, "create token request")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, oops.In("auth").Code("codex_token_http").Wrapf(err, "request token")
	}
	defer closeAuthBody(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, oops.In("auth").
			Code("codex_token_status").
			With("status", response.StatusCode).
			Errorf("token request failed")
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	decodeErr := json.NewDecoder(response.Body).Decode(&payload)
	if decodeErr != nil {
		return nil, oops.In("auth").Code("codex_token_decode").Wrapf(decodeErr, "decode token response")
	}
	if payload.AccessToken == "" || payload.RefreshToken == "" || payload.ExpiresIn <= 0 {
		return nil, oops.In("auth").Code("codex_token_invalid").Errorf("token response is incomplete")
	}
	accountID, err := accountIDFromJWT(payload.AccessToken)
	if err != nil {
		return nil, err
	}

	return &Credential{
		OAuth:     nil,
		Type:      CredentialTypeOAuth,
		Key:       "",
		Access:    payload.AccessToken,
		Refresh:   payload.RefreshToken,
		AccountID: accountID,
		Expires:   time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second).UnixMilli(),
		ExpiresAt: 0,
	}, nil
}

func generatePKCE() (verifier, challenge string, err error) {
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return "", "", oops.In("auth").Code("pkce_random").Wrapf(err, "generate pkce verifier")
	}
	verifier = base64.RawURLEncoding.EncodeToString(verifierBytes)
	hash := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hash[:])

	return verifier, challenge, nil
}

func randomHex(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", oops.In("auth").Code("random_hex").Wrapf(err, "generate random state")
	}

	return hex.EncodeToString(bytes), nil
}

func accountIDFromJWT(token string) (string, error) {
	payload, err := jwtPayload(token)
	if err != nil {
		return "", err
	}
	authClaims, ok := payload[openAICodexJWTClaim].(map[string]any)
	if !ok {
		return "", oops.In("auth").Code("codex_account_claim").Errorf("account claim missing")
	}
	accountID, ok := authClaims["chatgpt_account_id"].(string)
	if !ok || accountID == "" {
		return "", oops.In("auth").Code("codex_account_id").Errorf("account id missing")
	}

	return accountID, nil
}

func jwtExpiresMillis(token string) int64 {
	payload, err := jwtPayload(token)
	if err != nil {
		return 0
	}
	expiresFloat, ok := payload["exp"].(float64)
	if ok {
		return int64(expiresFloat) * 1000
	}
	expiresString, ok := payload["exp"].(string)
	if !ok {
		return 0
	}
	expires, err := strconv.ParseInt(expiresString, 10, 64)
	if err != nil {
		return 0
	}

	return expires * 1000
}

func jwtPayload(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, oops.In("auth").Code("jwt_parts").Errorf("invalid jwt")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, oops.In("auth").Code("jwt_decode").Wrapf(err, "decode jwt payload")
	}
	var payload map[string]any
	decodeErr := json.Unmarshal(decoded, &payload)
	if decodeErr != nil {
		return nil, oops.In("auth").Code("jwt_json").Wrapf(decodeErr, "parse jwt payload")
	}

	return payload, nil
}

func openAICodexCredentialFromNativeFile(path string) (*Credential, bool) {
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, false
	}
	var payload struct {
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	decodeErr := json.Unmarshal(content, &payload)
	if decodeErr != nil {
		return nil, false
	}
	if payload.Tokens.AccessToken == "" || payload.Tokens.RefreshToken == "" {
		return nil, false
	}
	accountID, err := accountIDFromJWT(payload.Tokens.AccessToken)
	if err != nil {
		return nil, false
	}

	return &Credential{
		OAuth:     nil,
		Type:      CredentialTypeOAuth,
		Key:       "",
		Access:    payload.Tokens.AccessToken,
		Refresh:   payload.Tokens.RefreshToken,
		AccountID: accountID,
		Expires:   jwtExpiresMillis(payload.Tokens.AccessToken),
		ExpiresAt: 0,
	}, true
}

func openAICodexCredentialFromAuthFile(path string) (*Credential, bool) {
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, false
	}
	credentials, err := parseCredentials(content)
	if err != nil {
		return nil, false
	}
	credential, ok := credentials[openAICodexProvider]
	if !ok || credential.Type != CredentialTypeOAuth {
		return nil, false
	}

	return &credential, true
}
