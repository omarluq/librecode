package tool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fetchTestExampleURL    = "https://example.com"
	fetchTestIgnoredFooter = "Ignore footer"
	fetchTestIgnoredHeader = "Ignore header"
	fetchTestPlainText     = "plain text"
	fetchTestTextPlain     = "text/plain"
	serverURLPlaceholder   = "{server_url}"
)

func TestFetchTool_Definition(t *testing.T) {
	t.Parallel()

	definition := NewFetchTool().Definition()

	assert.Equal(t, NameFetch, definition.Name)
	assert.Equal(t, "fetch", definition.Label)
	assert.True(t, definition.ReadOnly)
	assert.NotEmpty(t, definition.Schema)
	assert.Contains(t, definition.Description, "Fetch an explicit HTTP(S) URL")
	assert.Contains(
		t,
		definition.PromptGuidelines,
		"Use fetch only for explicit URLs that are relevant to the user's task.",
	)
}

func TestFetchTool_FetchHTMLFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		format          string
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:            "default markdown",
			format:          "",
			wantContains:    []string{"# Hello", "Useful", serverURLPlaceholder + "/docs)"},
			wantNotContains: []string{fetchTestIgnoredHeader, fetchTestIgnoredFooter, "window.bad"},
		},
		{
			name:            "text",
			format:          fetchFormatText,
			wantContains:    []string{"Hello Useful docs."},
			wantNotContains: []string{fetchTestIgnoredHeader, fetchTestIgnoredFooter, "window.bad"},
		},
		{
			name:            "html",
			format:          fetchFormatHTML,
			wantContains:    []string{"<html>", "<h1>Hello</h1>", `<a href="/docs">docs</a>`},
			wantNotContains: []string{fetchTestIgnoredHeader, fetchTestIgnoredFooter, "<script>"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := fetchTestHTMLServer(t)
			defer server.Close()

			wantContains := replaceFetchServerURL(testCase.wantContains, server.URL)
			input := fetchInputForTest(server.URL, testCase.format)
			result, err := fetchTestPrivateNetworkTool().Fetch(context.Background(), input)

			require.NoError(t, err)

			assertFetchTextContains(t, result, wantContains)
			assertFetchTextNotContains(t, result, testCase.wantNotContains)

			assert.Equal(t, server.URL, result.Details["url"])
			assert.Equal(t, server.URL, result.Details["final_url"])
			assert.Equal(t, http.StatusOK, result.Details["status"])
			assert.Equal(t, fetchHTMLContentType, result.Details["content_type"])
			assert.Equal(t, normalizeFetchFormatForTest(testCase.format), result.Details["format"])
			assert.Equal(t, "Fetch Title", result.Details["title"])
			assert.False(t, fetchDetailBoolForTest(t, result, "truncated"))
		})
	}
}

func TestFetchTool_FetchNonHTMLFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		body        string
		format      string
		want        string
	}{
		{
			name:        "json markdown",
			contentType: "application/json; charset=utf-8",
			body:        `{"z":1,"html":"<b>safe</b>"}`,
			format:      fetchFormatMarkdown,
			want:        "```json\n{\n  \"html\": \"<b>safe</b>\",\n  \"z\": 1\n}\n```",
		},
		{
			name:        "json text",
			contentType: fetchJSONContentType,
			body:        `{"ok":true}`,
			format:      fetchFormatText,
			want:        "{\n  \"ok\": true\n}",
		},
		{
			name:        "plain markdown",
			contentType: fetchTestTextPlain,
			body:        fetchTestPlainText,
			format:      fetchFormatMarkdown,
			want:        "```text\nplain text\n```",
		},
		{
			name:        "plain markdown with backticks",
			contentType: fetchTestTextPlain,
			body:        "contains ``` fenced content",
			format:      fetchFormatMarkdown,
			want:        "````text\ncontains ``` fenced content\n````",
		},
		{
			name:        "plain html format",
			contentType: fetchTestTextPlain,
			body:        fetchTestPlainText,
			format:      fetchFormatHTML,
			want:        fetchTestPlainText,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := fetchTestContentServer(t, testCase.contentType, []byte(testCase.body), http.StatusOK)
			defer server.Close()

			result, err := fetchTestPrivateNetworkTool().Fetch(
				context.Background(),
				fetchInputForTest(server.URL, testCase.format),
			)

			require.NoError(t, err)
			assert.Equal(t, testCase.want, result.Text())
		})
	}
}

func TestFetchTool_ValidationErrors(t *testing.T) {
	t.Parallel()

	timeoutZero := 0
	tests := []struct {
		name    string
		input   FetchInput
		wantErr string
	}{
		{name: "missing url", input: fetchInputForTest("", ""), wantErr: "fetch url is required"},
		{name: "invalid url", input: fetchInputForTest("http://%zz", ""), wantErr: "parse fetch url"},
		{name: "unsupported scheme", input: fetchInputForTest("file:///tmp/a", ""), wantErr: "http or https"},
		{name: "missing host", input: fetchInputForTest("https:///path", ""), wantErr: "host is required"},
		{
			name:    "invalid format",
			input:   fetchInputForTest(fetchTestExampleURL, "pdf"),
			wantErr: "format must be markdown",
		},
		{
			name:    "invalid timeout",
			input:   FetchInput{Timeout: &timeoutZero, URL: fetchTestExampleURL, Format: ""},
			wantErr: "timeout must be greater",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewFetchTool().Fetch(context.Background(), testCase.input)

			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErr)
		})
	}
}

func TestFetchTool_HTTPErrorAndInvalidUTF8(t *testing.T) {
	t.Parallel()

	t.Run("non 2xx", func(t *testing.T) {
		t.Parallel()

		server := fetchTestContentServer(t, fetchTestTextPlain, []byte("teapot"), http.StatusTeapot)
		defer server.Close()

		_, err := fetchTestPrivateNetworkTool().Fetch(context.Background(), fetchInputForTest(server.URL, ""))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "418")
	})

	t.Run("invalid utf8", func(t *testing.T) {
		t.Parallel()

		server := fetchTestContentServer(t, fetchTestTextPlain, []byte{0xff, 0xfe}, http.StatusOK)
		defer server.Close()

		_, err := fetchTestPrivateNetworkTool().Fetch(context.Background(), fetchInputForTest(server.URL, ""))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not valid UTF-8")
	})
}

func TestFetchTool_TextOutputWrapsLongHTMLText(t *testing.T) {
	t.Parallel()

	server := fetchTestContentServer(
		t,
		fetchHTMLContentType,
		[]byte("<html><body>"+strings.Repeat("word ", 80)+"</body></html>"),
		http.StatusOK,
	)
	defer server.Close()

	result, err := fetchTestPrivateNetworkTool().Fetch(
		context.Background(),
		fetchInputForTest(server.URL, fetchFormatText),
	)

	require.NoError(t, err)
	assert.Contains(t, result.Text(), "\n")
	assert.LessOrEqual(t, len(strings.Split(result.Text(), "\n")[0]), fetchTextWrapWidth)
}

func TestFetchTool_RedirectAndTruncationDetails(t *testing.T) {
	t.Parallel()

	server := fetchTestPrivateNetworkServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/start" {
			http.Redirect(writer, request, "/final", http.StatusFound)

			return
		}

		writer.Header().Set("Content-Type", fetchTestTextPlain)

		_, err := writer.Write([]byte(strings.Repeat("line\n", DefaultMaxLines+1)))
		if err != nil {
			panic(err)
		}
	}))
	defer server.Close()

	result, err := fetchTestPrivateNetworkTool().Fetch(
		context.Background(),
		fetchInputForTest(server.URL+"/start", fetchFormatText),
	)

	require.NoError(t, err)
	assert.Equal(t, server.URL+"/start", result.Details["url"])
	assert.Equal(t, server.URL+"/final", result.Details["final_url"])
	assert.True(t, fetchDetailBoolForTest(t, result, "truncated"))
	assert.Contains(t, result.Details, detailTruncation)
	assert.Contains(t, result.Text(), "Showing first")
}

func TestFetchTool_TruncatesSingleLongLine(t *testing.T) {
	t.Parallel()

	server := fetchTestContentServer(
		t,
		fetchTestTextPlain,
		[]byte(strings.Repeat("a", DefaultMaxBytes+1)),
		http.StatusOK,
	)
	defer server.Close()

	result, err := fetchTestPrivateNetworkTool().Fetch(
		context.Background(),
		fetchInputForTest(server.URL, fetchFormatText),
	)

	require.NoError(t, err)
	assert.Len(t, result.Text(), DefaultMaxBytes+len("\n\n[Showing first 1 lines of 1 (50KiB limit).]"))
	assert.True(t, fetchDetailBoolForTest(t, result, "truncated"))
	assert.Contains(t, result.Text(), "Showing first 1 lines")
}

func TestFetchTool_ReadLimitDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", fetchTestTextPlain)

		_, err := writer.Write([]byte(strings.Repeat("a", fetchReadLimitBytes+1)))
		if err != nil {
			panic(err)
		}
	}))
	defer server.Close()

	result, err := fetchTestPrivateNetworkTool().Fetch(
		context.Background(),
		fetchInputForTest(server.URL, fetchFormatText),
	)

	require.NoError(t, err)
	assert.Equal(t, fetchReadLimitBytes, result.Details["bytes_read"])
	assert.True(t, fetchDetailBoolForTest(t, result, "read_limit_reached"))
	assert.True(t, fetchDetailBoolForTest(t, result, "truncated"))
}

func TestFetchTool_RejectsPrivateNetworkTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		lookups map[string][]net.IPAddr
		name    string
		rawURL  string
	}{
		{
			lookups: nil,
			name:    "localhost hostname",
			rawURL:  "http://localhost",
		},
		{
			lookups: nil,
			name:    "loopback ip",
			rawURL:  "http://127.0.0.1",
		},
		{
			lookups: nil,
			name:    "private ip",
			rawURL:  "http://10.0.0.1",
		},
		{
			lookups: nil,
			name:    "link local ip",
			rawURL:  "http://169.254.1.1",
		},
		{
			lookups: map[string][]net.IPAddr{
				"example.test": {{IP: net.ParseIP("192.168.1.10")}},
			},
			name:   "private dns result",
			rawURL: "http://example.test",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fetchTool := fetchTestLookupTool(testCase.lookups)
			_, err := fetchTool.Fetch(context.Background(), fetchInputForTest(testCase.rawURL, ""))

			require.Error(t, err)
			assert.Contains(t, err.Error(), "private or local networks")
		})
	}
}

func TestFetchTool_RejectsPrivateNetworkRedirect(t *testing.T) {
	t.Parallel()

	fetchTool := fetchTestLookupTool(map[string][]net.IPAddr{
		"example.test": {{IP: net.ParseIP("93.184.216.34")}},
	})
	fetchTool.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := fetchTestHTTPResponse(request, io.NopCloser(strings.NewReader("redirect")))
		response.StatusCode = http.StatusFound
		response.Status = "302 Found"
		response.Header.Set("Location", "http://127.0.0.1/final")

		return response, nil
	})}

	_, err := fetchTool.Fetch(context.Background(), fetchInputForTest("http://example.test/start", ""))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "private or local networks")
}

func TestFetchTool_HTTPClientErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		transport   http.RoundTripper
		wantErrText string
	}{
		{
			name: "request error",
			transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("network down")
			}),
			wantErrText: "fetch url",
		},
		{
			name: "body read error",
			transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body := fetchTestErrorBody{
					reader:   nil,
					readErr:  errors.New("read failed"),
					closeErr: nil,
				}

				return fetchTestHTTPResponse(request, body), nil
			}),
			wantErrText: "read fetch response",
		},
		{
			name: "body close error",
			transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body := fetchTestErrorBody{
					reader:   strings.NewReader("ok"),
					readErr:  nil,
					closeErr: errors.New("close failed"),
				}

				return fetchTestHTTPResponse(request, body), nil
			}),
			wantErrText: "close fetch response",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fetchTool := fetchTestPrivateNetworkTool()
			fetchTool.client = &http.Client{Transport: testCase.transport}
			_, err := fetchTool.Fetch(context.Background(), fetchInputForTest(fetchTestExampleURL, ""))

			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErrText)
		})
	}
}

func TestFetchTool_FencedCodeBlockUsesSafeFence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{name: "no backticks", content: "plain", expected: "```text\nplain\n```"},
		{name: "short run", content: "`code`", expected: "```text\n`code`\n```"},
		{name: "triple run", content: "```code```", expected: "````text\n```code```\n````"},
		{name: "longer run", content: "````code````", expected: "`````text\n````code````\n`````"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, fencedCodeBlock("text", testCase.content))
		})
	}
}

func TestFetchTool_ContentTypeDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		header   string
		wantType string
		wantHTML bool
		wantJSON bool
	}{
		{
			name:     "html with charset",
			header:   "Text/HTML; charset=utf-8",
			wantType: fetchHTMLContentType,
			wantHTML: true,
			wantJSON: false,
		},
		{
			name:     "xhtml",
			header:   fetchXHTMLContentType,
			wantType: fetchXHTMLContentType,
			wantHTML: true,
			wantJSON: false,
		},
		{
			name:     "html suffix",
			header:   "application/custom+html",
			wantType: "application/custom+html",
			wantHTML: true,
			wantJSON: false,
		},
		{
			name:     "json with charset",
			header:   "Application/JSON; charset=utf-8",
			wantType: fetchJSONContentType,
			wantHTML: false,
			wantJSON: true,
		},
		{
			name:     "json suffix",
			header:   "application/problem+json",
			wantType: "application/problem+json",
			wantHTML: false,
			wantJSON: true,
		},
		{
			name:     "invalid media type fallback",
			header:   "text/plain; charset",
			wantType: "text/plain; charset",
			wantHTML: false,
			wantJSON: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			contentType := normalizedFetchContentType(testCase.header)

			assert.Equal(t, testCase.wantType, contentType)
			assert.Equal(t, testCase.wantHTML, isFetchHTML(contentType))
			assert.Equal(t, testCase.wantJSON, isFetchJSON(contentType))
		})
	}
}

func TestFetchTool_ExecuteAndTimeoutClamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		timeout     int
		wantTimeout time.Duration
	}{
		{name: "within range", timeout: 2, wantTimeout: 2 * time.Second},
		{name: "above max", timeout: 999, wantTimeout: fetchMaxTimeout},
		{name: "max int", timeout: math.MaxInt, wantTimeout: fetchMaxTimeout},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			args, err := ArgumentsFromRaw(fmt.Appendf(
				nil,
				`{"url":"https://example.com","timeout":%d}`,
				testCase.timeout,
			))
			require.NoError(t, err)

			tool := fetchTestLookupTool(map[string][]net.IPAddr{
				"example.com": {{IP: net.ParseIP("93.184.216.34")}},
			})
			tool.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				deadline, ok := request.Context().Deadline()
				require.True(t, ok)
				assert.LessOrEqual(t, time.Until(deadline), testCase.wantTimeout)

				return fetchTestHTTPResponse(request, io.NopCloser(strings.NewReader("ok"))), nil
			})}

			result, err := tool.Execute(context.Background(), args)

			require.NoError(t, err)
			assert.Equal(t, "```text\nok\n```", result.Text())
		})
	}
}

func fetchTestHTMLServer(t *testing.T) *httptest.Server {
	t.Helper()

	return fetchTestPrivateNetworkServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, fetchUserAgent, request.Header.Get("User-Agent"))
		assert.NotEmpty(t, request.Header.Get("Accept"))
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")

		_, err := writer.Write([]byte(strings.Join([]string{
			"<!doctype html>",
			"<html>",
			"<head><title>Fetch Title</title><style>.hidden{}</style><script>window.bad = true</script></head>",
			"<body><header>Ignore header</header><main><h1>Hello</h1>",
			`<p>Useful <a href="/docs">docs</a>.</p></main><footer>Ignore footer</footer></body>`,
			"</html>",
		}, "\n")))
		if err != nil {
			panic(err)
		}
	}))
}

func fetchTestContentServer(t *testing.T, contentType string, body []byte, status int) *httptest.Server {
	t.Helper()

	return fetchTestPrivateNetworkServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", contentType)
		writer.WriteHeader(status)

		_, err := writer.Write(body)
		if err != nil {
			panic(err)
		}
	}))
}

func fetchTestPrivateNetworkServer(handler http.Handler) *httptest.Server {
	server := httptest.NewServer(handler)

	return server
}

func fetchTestPrivateNetworkTool() *FetchTool {
	fetchTool := NewFetchTool()
	fetchTool.allowPrivateNetworks = true

	return fetchTool
}

func fetchTestLookupTool(lookups map[string][]net.IPAddr) *FetchTool {
	fetchTool := NewFetchTool()
	fetchTool.lookupIPAddrs = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if addrs, ok := lookups[host]; ok {
			return addrs, nil
		}

		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}

	return fetchTool
}

func assertFetchTextContains(t *testing.T, result Result, expectedValues []string) {
	t.Helper()

	for _, expected := range expectedValues {
		assert.Contains(t, result.Text(), expected)
	}
}

func assertFetchTextNotContains(t *testing.T, result Result, unexpectedValues []string) {
	t.Helper()

	for _, unexpected := range unexpectedValues {
		assert.NotContains(t, result.Text(), unexpected)
	}
}

func replaceFetchServerURL(values []string, serverURL string) []string {
	replaced := make([]string, 0, len(values))
	for _, value := range values {
		replaced = append(replaced, strings.ReplaceAll(value, serverURLPlaceholder, serverURL))
	}

	return replaced
}

func fetchInputForTest(rawURL, format string) FetchInput {
	return FetchInput{Timeout: nil, URL: rawURL, Format: format}
}

func fetchDetailBoolForTest(t *testing.T, result Result, key string) bool {
	t.Helper()

	value, ok := result.Details[key].(bool)
	require.True(t, ok)

	return value
}

func normalizeFetchFormatForTest(format string) string {
	if format == "" {
		return fetchDefaultFormat
	}

	return format
}

func fetchTestHTTPResponse(request *http.Request, body io.ReadCloser) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{fetchTestTextPlain}},
		Body:       body,
		Request:    request,
	}
}

type fetchTestErrorBody struct {
	reader   io.Reader
	readErr  error
	closeErr error
}

func (body fetchTestErrorBody) Read(target []byte) (int, error) {
	if body.readErr != nil {
		return 0, body.readErr
	}

	if body.reader == nil {
		return 0, io.EOF
	}

	count, err := body.reader.Read(target)
	if err == nil {
		return count, nil
	}

	if errors.Is(err, io.EOF) {
		return count, io.EOF
	}

	return count, errors.Join(err)
}

func (body fetchTestErrorBody) Close() error {
	return body.closeErr
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}
