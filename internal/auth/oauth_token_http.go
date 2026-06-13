package auth

import (
	"net/http"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/limitio"
	"github.com/omarluq/librecode/internal/units"
)

func doOAuthTokenRequest(request *http.Request, responseLabel, codePrefix string) ([]byte, error) {
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, oops.In("auth").Code(codePrefix+"_http").Wrapf(err, "request token")
	}
	defer closeAuthBody(response.Body)

	body, err := limitio.ReadAll(response.Body, units.MiB, responseLabel)
	if err != nil {
		return nil, oops.In("auth").Code(codePrefix+"_body").Wrapf(err, "read token response")
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, oops.In("auth").
			Code(codePrefix+"_status").
			With("status", response.StatusCode).
			Errorf("token request failed: %s", strings.TrimSpace(string(body)))
	}

	return body, nil
}
