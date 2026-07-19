package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MicahParks/jwkset"
	"github.com/MicahParks/keyfunc/v3"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/jwtclaim"
)

const (
	openAICodexProvider    = "openai-codex"
	openAICodexClientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	openAICodexAuthorize   = "https://auth.openai.com/oauth/authorize"
	openAICodexJWKSetURL   = "https://auth.openai.com/.well-known/jwks.json"
	openAICodexRedirectURI = "http://localhost:1455/auth/callback"
	openAICodexScope       = "openid profile email offline_access"
	openAICodexJWTClaim    = "https://api.openai.com/auth"
	openAICodexCallback    = "127.0.0.1:1455"

	oauthStateBytes      = 16
	oauthVerifierBytes   = 32
	oauthCallbackTimeout = 5 * time.Second
	oauthShutdownTimeout = 2 * time.Second
	oauthTokenTimeout    = 30 * time.Second
)

func openAICodexExchangeEndpoint() string {
	return "https://auth.openai.com/oauth/token"
}

// OAuthAuthInfo describes a browser OAuth step.
type OAuthAuthInfo struct {
	URL          string `json:"url"`
	Instructions string `json:"instructions,omitempty"`
}

// LoginOpenAICodex runs the ChatGPT/Codex OAuth browser flow.
func LoginOpenAICodex(ctx context.Context, onAuth func(OAuthAuthInfo)) (credential *Credential, err error) {
	flow, err := newOpenAICodexFlow()
	if err != nil {
		return nil, err
	}

	server, err := startOpenAICodexCallbackServer(flow.State)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, server.Close(ctx))
	}()

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

	credential, err = exchangeOpenAICodexCode(ctx, code, flow.Verifier)
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
	return refreshOpenAICodexWithTokenURL(ctx, refreshToken, openAICodexExchangeEndpoint())
}

func refreshOpenAICodexWithTokenURL(ctx context.Context, refreshToken, tokenURL string) (*Credential, error) {
	return refreshOpenAICodexWithTokenURLAndParser(ctx, refreshToken, tokenURL, parseOpenAICodexJWT)
}

func refreshOpenAICodexWithTokenURLAndParser(
	ctx context.Context,
	refreshToken string,
	tokenURL string,
	parseJWT openAICodexJWTParser,
) (*Credential, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)
	values.Set("client_id", openAICodexClientID)

	return requestOpenAICodexToken(ctx, values, tokenURL, parseJWT)
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

	state, err := randomHex(oauthStateBytes)
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

type oauthCallbackServer struct {
	server     *http.Server
	codes      chan callbackResult
	codePrefix string
}

type openAICodexCallbackServer = oauthCallbackServer

type callbackResult struct {
	Err  error
	Code string
}

func startOpenAICodexCallbackServer(state string) (*openAICodexCallbackServer, error) {
	return startOAuthCallbackServer(openAICodexCallback, "/auth/callback", state, "codex")
}

func startOAuthCallbackServer(address, callbackPath, state, codePrefix string) (*oauthCallbackServer, error) {
	codes := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, oauthCallbackHandler(state, codes))
	mux.HandleFunc("/", func(writer http.ResponseWriter, _ *http.Request) {
		writeOAuthHTML(writer, http.StatusNotFound, "Callback route not found.")
	})

	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: oauthCallbackTimeout,
	}

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", address)
	if err != nil {
		return nil, oops.In("auth").Code(codePrefix+"_callback_listen").Wrapf(err, "listen for oauth callback")
	}

	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			codes <- callbackResult{Code: "", Err: serveErr}
		}
	}()

	return &oauthCallbackServer{server: server, codes: codes, codePrefix: codePrefix}, nil
}

func oauthCallbackHandler(state string, codes chan<- callbackResult) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("state") != state {
			writeOAuthHTML(writer, http.StatusBadRequest, "Authentication state mismatch.")

			codes <- callbackResult{Code: "", Err: errors.New("state mismatch")}

			return
		}

		code := query.Get("code")
		if code == "" {
			writeOAuthHTML(writer, http.StatusBadRequest, "Missing authorization code.")

			codes <- callbackResult{Code: "", Err: errors.New("missing authorization code")}

			return
		}

		writeOAuthHTML(writer, http.StatusOK, "librecode authentication complete. You can close this tab.")

		codes <- callbackResult{Code: code, Err: nil}
	}
}

func (server *oauthCallbackServer) Wait(ctx context.Context) (string, error) {
	select {
	case result := <-server.codes:
		if result.Err != nil {
			return "", oops.In("auth").Code(server.codePrefix+"_callback").Wrapf(result.Err, "receive oauth callback")
		}

		return result.Code, nil
	case <-ctx.Done():
		return "", oops.In("auth").Code(server.codePrefix+"_callback_canceled").Wrapf(
			ctx.Err(),
			"wait for oauth callback",
		)
	}
}

func (server *oauthCallbackServer) Close(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, oauthShutdownTimeout)
	defer cancel()

	shutdownErr := server.server.Shutdown(shutdownCtx)
	if shutdownErr == nil {
		return nil
	}

	closeErr := server.server.Close()

	return oops.In("auth").Code(server.codePrefix+"_callback_shutdown").Wrapf(
		errors.Join(shutdownErr, closeErr),
		"shut down oauth callback server",
	)
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
	return exchangeOpenAICodexCodeWithTokenURL(ctx, code, verifier, openAICodexExchangeEndpoint())
}

func exchangeOpenAICodexCodeWithTokenURL(ctx context.Context, code, verifier, tokenURL string) (*Credential, error) {
	return exchangeOpenAICodexCodeWithTokenURLAndParser(ctx, code, verifier, tokenURL, parseOpenAICodexJWT)
}

func exchangeOpenAICodexCodeWithTokenURLAndParser(
	ctx context.Context,
	code string,
	verifier string,
	tokenURL string,
	parseJWT openAICodexJWTParser,
) (*Credential, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("client_id", openAICodexClientID)
	values.Set("code", code)
	values.Set("code_verifier", verifier)
	values.Set("redirect_uri", openAICodexRedirectURI)

	return requestOpenAICodexToken(ctx, values, tokenURL, parseJWT)
}

type openAICodexJWTParser func(ctx context.Context, token string) (map[string]any, error)

func requestOpenAICodexToken(
	ctx context.Context,
	values url.Values,
	tokenURL string,
	parseJWT openAICodexJWTParser,
) (*Credential, error) {
	body, err := postOpenAICodexToken(ctx, values, tokenURL)
	if err != nil {
		return nil, err
	}

	return decodeOpenAICodexToken(ctx, body, parseJWT)
}

func postOpenAICodexToken(ctx context.Context, values url.Values, tokenURL string) ([]byte, error) {
	requestCtx, cancel := context.WithTimeout(ctx, oauthTokenTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		tokenURL,
		strings.NewReader(values.Encode()),
	)
	if err != nil {
		return nil, oops.In("auth").Code("codex_token_request").Wrapf(err, "create token request")
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return doOAuthTokenRequest(request, "codex token response", "codex_token")
}

func decodeOpenAICodexToken(ctx context.Context, body []byte, parseJWT openAICodexJWTParser) (*Credential, error) {
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}

	decodeErr := json.Unmarshal(body, &payload)
	if decodeErr != nil {
		return nil, oops.In("auth").Code("codex_token_decode").Wrapf(decodeErr, "decode token response")
	}

	if payload.AccessToken == "" || payload.RefreshToken == "" || payload.ExpiresIn <= 0 {
		return nil, oops.In("auth").Code("codex_token_invalid").Errorf("token response is incomplete")
	}

	accountID, err := accountIDFromJWT(ctx, payload.AccessToken, parseJWT)
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
	verifierBytes := make([]byte, oauthVerifierBytes)
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

func accountIDFromJWT(ctx context.Context, token string, parseJWT openAICodexJWTParser) (string, error) {
	claims, err := parseJWT(ctx, token)
	if err != nil {
		return "", err
	}

	authClaims, matched := claims[openAICodexJWTClaim].(map[string]any)
	if !matched {
		return "", oops.In("auth").Code("codex_account_claim").Errorf("account claim missing")
	}

	accountID, matched := authClaims["chatgpt_account_id"].(string)
	if !matched || accountID == "" {
		return "", oops.In("auth").Code("codex_account_id").Errorf("account id missing")
	}

	return accountID, nil
}

func parseOpenAICodexJWT(ctx context.Context, token string) (map[string]any, error) {
	return parseOpenAICodexJWTWithJWKSetURL(ctx, token, openAICodexJWKSetURL)
}

func parseOpenAICodexJWTWithJWKSetURL(ctx context.Context, token, jwkSetURL string) (map[string]any, error) {
	storage, err := jwkset.NewStorageFromHTTP(jwkSetURL, jwkset.HTTPClientStorageOptions{
		Ctx: ctx,
		ValidateOptions: jwkset.JWKValidateOptions{
			SkipAll: true,
		},
	})
	if err != nil {
		return nil, oops.In("auth").Code("jwt_keys").Wrapf(err, "load jwt verification keys")
	}

	keyFunc, err := keyfunc.New(keyfunc.Options{
		Storage:      storage,
		UseWhitelist: []jwkset.USE{jwkset.UseSig},
	})
	if err != nil {
		return nil, oops.In("auth").Code("jwt_keys").Wrapf(err, "load jwt verification keys")
	}

	claims, err := jwtclaim.ParseClaims(token, keyFunc.Keyfunc)
	if err != nil {
		return nil, oops.In("auth").Code("jwt_parse").Wrapf(err, "parse jwt payload")
	}

	return claims, nil
}
