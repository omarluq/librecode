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
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

const (
	fetchTestExampleHost    = "example.test"
	fetchTestNetworkTCP     = "tcp"
	fetchTestNetworkTCP6    = "tcp6"
	fetchTestWantNoAddrErr  = "returned no addresses"
	fetchTestWantPrivateErr = "private or local networks"
	fetchTestExampleURL     = "https://example.com"
	fetchTestIgnoredFooter  = "Ignore footer"
	fetchTestIgnoredHeader  = "Ignore header"
	fetchTestPlainText      = "plain text"
	fetchTestTextPlain      = "text/plain"
	fetchTestInvalidLimit   = "invalid limit"
	serverURLPlaceholder    = "{server_url}"
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
	offsetZero := 0
	limitZero := 0
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
			name: "invalid timeout",
			input: FetchInput{
				Timeout: &timeoutZero,
				Offset:  nil,
				Limit:   nil,
				URL:     fetchTestExampleURL,
				Format:  "",
			},
			wantErr: "timeout must be greater",
		},
		{
			name: "invalid offset",
			input: FetchInput{
				Timeout: nil,
				Offset:  &offsetZero,
				Limit:   nil,
				URL:     fetchTestExampleURL,
				Format:  "",
			},
			wantErr: "offset must be greater",
		},
		{
			name: fetchTestInvalidLimit,
			input: FetchInput{
				Timeout: nil,
				Offset:  nil,
				Limit:   &limitZero,
				URL:     fetchTestExampleURL,
				Format:  "",
			},
			wantErr: "limit must be greater",
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

func TestFetchTool_OffsetAndLimit(t *testing.T) {
	t.Parallel()

	lineOffset := 2
	lineLimit := 2

	server := fetchTestContentServer(t, fetchTestTextPlain, []byte("one\ntwo\nthree\nfour"), http.StatusOK)
	defer server.Close()

	result, err := fetchTestPrivateNetworkTool().Fetch(
		context.Background(),
		FetchInput{
			Timeout: nil,
			Offset:  &lineOffset,
			Limit:   &lineLimit,
			URL:     server.URL,
			Format:  fetchFormatText,
		},
	)

	require.NoError(t, err)
	assert.Equal(t, "two\nthree\n\n[1 more lines in fetched output. Use offset=4 to continue.]", result.Text())
	assert.Equal(t, 2, result.Details["offset"])
	assert.Equal(t, 2, result.Details["limit"])
	assert.Equal(t, 4, result.Details["total_lines"])
}

func TestFetchTool_OffsetBeyondOutput(t *testing.T) {
	t.Parallel()

	lineOffset := 3

	server := fetchTestContentServer(t, fetchTestTextPlain, []byte("one\ntwo"), http.StatusOK)
	defer server.Close()

	_, err := fetchTestPrivateNetworkTool().Fetch(
		context.Background(),
		FetchInput{
			Timeout: nil,
			Offset:  &lineOffset,
			Limit:   nil,
			URL:     server.URL,
			Format:  fetchFormatText,
		},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "offset is beyond fetched output")
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
	assert.Contains(t, result.Text(), "Showing lines 1-2000")
	assert.Contains(t, result.Text(), "Use offset=2001 to continue")
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

	truncationNotice := "\n\n[Showing lines 1-1 of 1 (50KiB limit). Use offset=2 to continue.]"
	assert.Len(t, result.Text(), DefaultMaxBytes+len(truncationNotice))
	assert.True(t, fetchDetailBoolForTest(t, result, "truncated"))
	assert.Contains(t, result.Text(), "Showing lines 1-1")
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

type fetchTestDialPinCase struct {
	wantDialedIP string
	wantErr      string
	name         string
	lookups      [][]net.IPAddr
}

func fetchTestDialPinCases() []fetchTestDialPinCase {
	cases := append([]fetchTestDialPinCase{
		{
			name:         "public host dials pinned validated ip",
			lookups:      [][]net.IPAddr{{{IP: net.ParseIP("93.184.216.34")}}},
			wantErr:      "",
			wantDialedIP: "93.184.216.34",
		},
		{
			name: "ambiguous empty re-resolution fails closed",
			lookups: [][]net.IPAddr{
				{{IP: net.ParseIP("93.184.216.34")}},
				{},
			},
			wantErr:      fetchTestWantNoAddrErr,
			wantDialedIP: "",
		},
	}, fetchTestRebindingDialPinCases()...)

	return cases
}

// fetchTestRebindingDialPinCases builds dial-rebinding cases sharing one shape:
// the pre-flight lookup returns the public example IP, then the dial-time
// re-resolution returns the hostile rebinding IP, which must be rejected before
// any connection is attempted. Generating the near-identical entries keeps the
// test table free of duplicated blocks.
func fetchTestRebindingDialPinCases() []fetchTestDialPinCase {
	targets := []struct {
		name string
		ip   string
	}{
		{"loopback", "127.0.0.1"},
		{"private network", "192.168.0.5"},
		{"link local metadata address", "169.254.169.254"},
	}

	cases := make([]fetchTestDialPinCase, 0, len(targets))
	for _, target := range targets {
		cases = append(cases, fetchTestDialPinCase{
			name: "resolver rebinding to " + target.name + " is rejected at dial time",
			lookups: [][]net.IPAddr{
				{{IP: net.ParseIP("93.184.216.34")}},
				{{IP: net.ParseIP(target.ip)}},
			},
			wantErr:      fetchTestWantPrivateErr,
			wantDialedIP: "",
		})
	}

	return cases
}

// fetchTestRunDialPinCase exercises one dial-pinning table entry end to end:
// validate, wrap the transport, dial via dialCase, and assert the recorded
// target is the pinned validated IP (or that dialing was refused).
type (
	fetchTestDialFunc = func(context.Context, string, string) (net.Conn, error)
	fetchTestDialHook = func(*http.Transport) fetchTestDialFunc
)

func fetchTestRunDialPinCase(t *testing.T, testCase fetchTestDialPinCase, port string, dialCase fetchTestDialHook) {
	t.Helper()

	lookupCalls := 0
	fetchTool, dialedAddresses := fetchTestRecordingTransport()
	fetchTool.lookupIPAddrs = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		defer func() { lookupCalls++ }()

		return testCase.lookups[min(lookupCalls, len(testCase.lookups)-1)], nil
	}

	requestURL, err := parseFetchURL("http://" + fetchTestExampleHost)
	require.NoError(t, err)

	// Consume the pre-flight validation lookup the same way the real
	// request path does, so the dial below observes the second resolution.
	require.NoError(t, fetchTool.validatePublicFetchURL(context.Background(), requestURL))

	transport, closeIdleConnections, err := fetchTool.transportWithNetworkValidation(fetchTool.client.Transport)
	require.NoError(t, err)

	defer closeIdleConnections()

	httpTransport, ok := transport.(*http.Transport)
	require.True(t, ok)

	conn, dialErr := dialCase(httpTransport)(context.Background(), "tcp", fetchTestExampleHost+":"+port)

	if testCase.wantErr != "" {
		require.Error(t, dialErr)
		assert.Contains(t, dialErr.Error(), testCase.wantErr)
		assert.Empty(t, *dialedAddresses, "no connection should be attempted after validation fails")

		return
	}

	require.NoError(t, dialErr)
	require.NoError(t, conn.Close())
	require.Len(t, *dialedAddresses, 1)
	assert.Equal(t, testCase.wantDialedIP+":"+port, (*dialedAddresses)[0])
}

func TestFetchTool_PinsDialedAddressToValidatedIP(t *testing.T) {
	t.Parallel()

	for _, testCase := range fetchTestDialPinCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fetchTestRunDialPinCase(t, testCase, "80", func(transport *http.Transport) fetchTestDialFunc {
				return transport.DialContext
			})
		})
	}
}

func TestFetchTool_PinsDialedTLSToValidatedIP(t *testing.T) {
	t.Parallel()

	for _, testCase := range fetchTestDialPinCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Supply a TLS dial hook so the validating wrapper exercises the
			// HTTPS path: the recorded target must be the pinned literal IP.
			fetchTestRunDialPinCase(t, testCase, "443", func(transport *http.Transport) fetchTestDialFunc {
				transport.DialTLSContext = transport.DialContext

				return transport.DialTLSContext
			})
		})
	}
}

// fetchTestRecordingTransport builds a fetch tool whose transport records every
// dialed address and rejects hostnames: the base dialer must only ever receive a
// validated literal IP, since a hostname would let the OS resolver rebind it.
func fetchTestRecordingTransport() (fetchTool *FetchTool, dialed *[]string) {
	fetchTool = NewFetchTool()

	var dialedAddresses []string

	fetchTool.client = &http.Client{Transport: &http.Transport{
		DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			dialedAddresses = append(dialedAddresses, address)

			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("split fetch dial address: %w", err)
			}

			ipAddress := parseFetchHostIP(host)
			if ipAddress == nil {
				return nil, fmt.Errorf("dialer received a hostname instead of a pinned IP: %s", host)
			}

			if err := validatePublicFetchIP(ipAddress); err != nil {
				return nil, fmt.Errorf("validate pinned dial address: %w", err)
			}

			remoteAddr, resolveErr := net.ResolveTCPAddr("tcp", address)
			if resolveErr != nil {
				return nil, fmt.Errorf("resolve pinned dial address: %w", resolveErr)
			}

			return fetchTestConn{remoteAddr: remoteAddr}, nil
		},
	}}

	return fetchTool, &dialedAddresses
}

type fetchTestPinnedDialCase struct {
	lookups map[string][]net.IPAddr
	name    string
	network string
	address string
	wantErr string
	wantPin string
}

func fetchTestPinnedDialAddressCases() []fetchTestPinnedDialCase {
	return append(fetchTestLiteralDialCases(), fetchTestResolvedDialCases()...)
}

func fetchTestLiteralDialCases() []fetchTestPinnedDialCase {
	return []fetchTestPinnedDialCase{
		{
			name:    "public literal ipv4 is pinned",
			network: fetchTestNetworkTCP,
			address: "93.184.216.34:443",
			lookups: nil,
			wantErr: "",
			wantPin: "93.184.216.34:443",
		},
		{
			name:    "public literal ipv6 is pinned with brackets",
			network: fetchTestNetworkTCP6,
			address: "[2606:2800:220:1:248:1893:25c8:1946]:443",
			lookups: nil,
			wantErr: "",
			wantPin: "[2606:2800:220:1:248:1893:25c8:1946]:443",
		},
		{
			name:    "private literal ipv4 is rejected",
			network: fetchTestNetworkTCP,
			address: "10.1.2.3:80",
			lookups: nil,
			wantErr: fetchTestWantPrivateErr,
			wantPin: "",
		},
		{
			name:    "link local literal ipv6 is rejected",
			network: fetchTestNetworkTCP6,
			address: "[fe80::1]:80",
			lookups: nil,
			wantErr: fetchTestWantPrivateErr,
			wantPin: "",
		},
		{
			name:    "localhost host is rejected",
			network: fetchTestNetworkTCP,
			address: "localhost:80",
			lookups: nil,
			wantErr: fetchTestWantPrivateErr,
			wantPin: "",
		},
		{
			name:    "missing port is rejected",
			network: fetchTestNetworkTCP,
			address: fetchTestExampleHost,
			lookups: nil,
			wantErr: "parse fetch dial address",
			wantPin: "",
		},
	}
}

func fetchTestResolvedDialCases() []fetchTestPinnedDialCase {
	const fetchTestExampleHostPort = fetchTestExampleHost + ":80"

	return []fetchTestPinnedDialCase{
		{
			name:    "hostname without validated addresses fails closed",
			network: fetchTestNetworkTCP,
			address: fetchTestExampleHostPort,
			lookups: map[string][]net.IPAddr{fetchTestExampleHost: {}},
			wantErr: fetchTestWantNoAddrErr,
			wantPin: "",
		},
		{
			name:    "ipv4 network with only ipv6 results fails closed",
			network: "tcp4",
			address: fetchTestExampleHostPort,
			lookups: map[string][]net.IPAddr{
				fetchTestExampleHost: {{IP: net.ParseIP("2606:2800:220:1:248:1893:25c8:1946")}},
			},
			wantErr: "no validated fetch address matches network",
			wantPin: "",
		},
		{
			name:    "hostname resolves to pinned public ipv6",
			network: fetchTestNetworkTCP6,
			address: fetchTestExampleHostPort,
			lookups: map[string][]net.IPAddr{
				fetchTestExampleHost: {
					{IP: net.ParseIP("93.184.216.34")},
					{IP: net.ParseIP("2606:2800:220:1:248:1893:25c8:1946")},
				},
			},
			wantErr: "",
			wantPin: "[2606:2800:220:1:248:1893:25c8:1946]:80",
		},
		{
			name:    "hostname with mixed public and private results is rejected",
			network: fetchTestNetworkTCP,
			address: fetchTestExampleHostPort,
			lookups: map[string][]net.IPAddr{
				fetchTestExampleHost: {{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("10.0.0.1")}},
			},
			wantErr: fetchTestWantPrivateErr,
			wantPin: "",
		},
		{
			name:    "dual stack tcp pins first validated address",
			network: fetchTestNetworkTCP,
			address: fetchTestExampleHostPort,
			lookups: map[string][]net.IPAddr{
				fetchTestExampleHost: {
					{IP: net.ParseIP("2606:2800:220:1:248:1893:25c8:1946")},
					{IP: net.ParseIP("93.184.216.34")},
				},
			},
			wantErr: "",
			wantPin: "[2606:2800:220:1:248:1893:25c8:1946]:80",
		},
		{
			name:    "trailing dot hostname is normalized",
			network: fetchTestNetworkTCP,
			address: "Example.Test.:80",
			// Distinct from the fetchTestLookupTool fallback IP so a
			// normalization regression hits the fallback and fails this case.
			lookups: map[string][]net.IPAddr{
				fetchTestExampleHost: {{IP: net.ParseIP("198.51.100.10")}},
			},
			wantErr: "",
			wantPin: "198.51.100.10:80",
		},
	}
}

func TestFetchTool_PinnedDialAddress(t *testing.T) {
	t.Parallel()

	for _, testCase := range fetchTestPinnedDialAddressCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fetchTool := fetchTestLookupTool(testCase.lookups)

			pinned, err := fetchTool.pinnedFetchDialAddress(context.Background(), testCase.network, testCase.address)

			if testCase.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.wantPin, pinned)
		})
	}
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
				fetchTestExampleHost: {{IP: net.ParseIP("192.168.1.10")}},
			},
			name:   "private dns result",
			rawURL: "http://" + fetchTestExampleHost,
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
		fetchTestExampleHost: {{IP: net.ParseIP("93.184.216.34")}},
	})
	fetchTool.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := fetchTestHTTPResponse(request, io.NopCloser(strings.NewReader("redirect")))
		response.StatusCode = http.StatusFound
		response.Status = "302 Found"
		response.Header.Set("Location", "http://127.0.0.1/final")

		return response, nil
	})}

	_, err := fetchTool.Fetch(context.Background(), fetchInputForTest("http://"+fetchTestExampleHost+"/start", ""))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "private or local networks")
}

func TestFetchTool_RejectsPrivateDialedAddress(t *testing.T) {
	t.Parallel()

	server := fetchTestPrivateNetworkServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if _, err := writer.Write([]byte("unexpected private target")); err != nil {
			return
		}
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	fetchTool := fetchTestLookupTool(map[string][]net.IPAddr{
		fetchTestExampleHost: {{IP: net.ParseIP("93.184.216.34")}},
	})
	fetchTool.client = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer

			return dialer.DialContext(ctx, network, serverURL.Host)
		},
	}}

	_, err = fetchTool.Fetch(context.Background(), fetchInputForTest("http://"+fetchTestExampleHost, ""))

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

func TestFetchTool_FetchFormattingErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mutate      func(*goquery.Document)
		name        string
		format      string
		wantErr     string
		wantContent string
	}{
		{
			mutate: func(doc *goquery.Document) {
				doc.Find("body").First().AppendNodes(fetchTestErrorNode())
			},
			name:        "markdown body render error",
			format:      fetchFormatMarkdown,
			wantErr:     fetchRenderHTMLContext,
			wantContent: "",
		},
		{
			mutate: func(doc *goquery.Document) {
				doc.Find("body").Remove()
				doc.Selection.Nodes[0].AppendChild(fetchTestErrorNode())
			},
			name:        "markdown document render error",
			format:      fetchFormatMarkdown,
			wantErr:     fetchRenderHTMLContext,
			wantContent: "",
		},
		{
			mutate: func(doc *goquery.Document) {
				doc.Find("body").Remove()
				doc.Selection.Nodes[0].AppendChild(fetchTestErrorNode())
			},
			name:        "html document render error",
			format:      fetchFormatHTML,
			wantErr:     fetchRenderHTMLContext,
			wantContent: "",
		},
		{
			mutate: func(doc *goquery.Document) {
				doc.Find("body").Remove()
			},
			name:        "html document fallback",
			format:      fetchFormatHTML,
			wantErr:     "",
			wantContent: "<html><head></head></html>",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			doc := fetchTestDocument(t, "<html><head></head><body><main>ok</main></body></html>")
			testCase.mutate(doc)

			var (
				content string
				err     error
			)
			if testCase.format == fetchFormatMarkdown {
				content, err = fetchedHTMLMarkdown(doc, fetchTestExampleURL)
			} else {
				content, err = fetchedHTMLBody(doc)
			}

			if testCase.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.wantContent, content)
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

func TestFetchTool_ExecuteDecodeError(t *testing.T) {
	t.Parallel()

	args, err := ArgumentsFromRaw([]byte(`{"url":123}`))
	require.NoError(t, err)

	result, err := NewFetchTool().Execute(context.Background(), args)

	require.Error(t, err)
	assert.Empty(t, result.Text())
}

func TestFetchTool_RedirectValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		baseCheckRedirect func(*http.Request, []*http.Request) error
		name              string
		wantErr           string
		via               []*http.Request
	}{
		{
			baseCheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("custom redirect denied")
			},
			name:    "base redirect policy error",
			via:     nil,
			wantErr: "custom redirect denied",
		},
		{
			baseCheckRedirect: nil,
			name:              "too many redirects",
			via:               make([]*http.Request, fetchMaxRedirects),
			wantErr:           "stopped after 10 redirects",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fetchTool := fetchTestPrivateNetworkTool()
			fetchTool.client = &http.Client{CheckRedirect: testCase.baseCheckRedirect}

			client, closeIdleConnections, err := fetchTool.httpClientWithRedirectValidation(context.Background())
			require.NoError(t, err)

			defer closeIdleConnections()

			request, err := http.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				fetchTestExampleURL,
				http.NoBody,
			)
			require.NoError(t, err)

			err = client.CheckRedirect(request, testCase.via)

			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErr)
		})
	}
}

// fetchTestLegacyDialTransport sets a deprecated legacy Dial hook via reflection,
// matching how the production guard detects it without tripping SA1019.
func fetchTestLegacyDialTransport() *http.Transport {
	transport := &http.Transport{}
	reflect.ValueOf(transport).Elem().FieldByName("Dial").Set(
		reflect.ValueOf(legacyFetchDial),
	)

	return transport
}

func fetchTestLegacyDialTLSTransport() *http.Transport {
	transport := &http.Transport{}
	reflect.ValueOf(transport).Elem().FieldByName("DialTLS").Set(
		reflect.ValueOf(legacyFetchDial),
	)

	return transport
}

func legacyFetchDial(string, string) (net.Conn, error) {
	return nil, errors.New("legacy dial should not run")
}

func TestFetchTool_RejectsLegacyTransportDialHooks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		transport func() *http.Transport
		name      string
	}{
		{name: "legacy dial", transport: fetchTestLegacyDialTransport},
		{name: "legacy dial tls", transport: fetchTestLegacyDialTLSTransport},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := NewFetchTool().transportWithNetworkValidation(testCase.transport())

			require.Error(t, err)
			assert.Contains(t, err.Error(), "deprecated legacy dial hooks")
		})
	}
}

func TestFetchTool_NetworkValidationEdges(t *testing.T) {
	t.Parallel()

	t.Run("nil client falls back to default", func(t *testing.T) {
		t.Parallel()

		fetchTool := NewFetchTool()
		fetchTool.client = nil

		assert.Same(t, http.DefaultClient, fetchTool.httpClient())
	})

	t.Run("build request error", func(t *testing.T) {
		t.Parallel()

		_, _, err := fetchTestPrivateNetworkTool().fetchURL(
			context.Background(),
			&url.URL{Scheme: "http", Host: "exa mple.com"},
			time.Second,
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "build fetch request")
	})

	t.Run("missing host validation", func(t *testing.T) {
		t.Parallel()

		err := NewFetchTool().validatePublicFetchURL(context.Background(), &url.URL{Scheme: "https"})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "host is required")
	})

	t.Run("lookup error", func(t *testing.T) {
		t.Parallel()

		fetchTool := NewFetchTool()
		fetchTool.lookupIPAddrs = func(context.Context, string) ([]net.IPAddr, error) {
			return nil, errors.New("dns unavailable")
		}
		requestURL, err := parseFetchURL(fetchTestExampleURL)
		require.NoError(t, err)

		err = fetchTool.validatePublicFetchURL(context.Background(), requestURL)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "dns unavailable")
	})

	t.Run("default resolver error", func(t *testing.T) {
		t.Parallel()

		_, err := NewFetchTool().lookupFetchIPAddrs(context.Background(), "bad host name")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolve fetch url host")
	})

	t.Run("lookup has no addresses", func(t *testing.T) {
		t.Parallel()

		fetchTool := fetchTestLookupTool(map[string][]net.IPAddr{"example.com": {}})
		requestURL, err := parseFetchURL(fetchTestExampleURL)
		require.NoError(t, err)

		err = fetchTool.validatePublicFetchURL(context.Background(), requestURL)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "returned no addresses")
	})

	t.Run("default resolver", func(t *testing.T) {
		t.Parallel()

		addrs, err := NewFetchTool().lookupFetchIPAddrs(context.Background(), "localhost")

		require.NoError(t, err)
		assert.NotEmpty(t, addrs)
	})
}

func TestFetchTool_URLAndRenderingHelpers(t *testing.T) {
	t.Parallel()

	t.Run("ip host zone is ignored", func(t *testing.T) {
		t.Parallel()

		assert.True(t, parseFetchHostIP("fe80::1%eth0").IsLinkLocalUnicast())
	})

	t.Run("invalid html format", func(t *testing.T) {
		t.Parallel()

		_, _, err := formatFetchedHTML("<html><body>ok</body></html>", fetchTestExampleURL, "xml")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "format must be markdown")
	})

	t.Run("blank lines collapse", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "one\n\ntwo", collapseBlankLines("one\n\n\n\ntwo"))
	})

	t.Run("empty text wraps to empty", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, wrapFetchedText(" \n\t "))
	})

	t.Run("utf8 prefix", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "content", validUTF8Prefix("content", 0))
		assert.Equal(t, "short", validUTF8Prefix("short", len("short")))
		assert.Empty(t, validUTF8Prefix("éclair", 1))
		assert.Equal(t, "é", validUTF8Prefix("éclair", len("é")))
	})

	t.Run("read limit notice without output truncation", func(t *testing.T) {
		t.Parallel()

		output := fetchOutputText(
			fetchTestTruncation("small"),
			fetchTestSelection(0, 1, nil),
			true,
		)

		assert.Contains(t, output, "Response read limit reached")
	})
}

func fetchTestDocument(t *testing.T, source string) *goquery.Document {
	t.Helper()

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(source))
	require.NoError(t, err)

	return doc
}

func fetchTestErrorNode() *html.Node {
	return &html.Node{Type: html.ErrorNode}
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

func fetchTestTruncation(content string) *TruncationResult {
	return &TruncationResult{
		Content:               content,
		TruncatedBy:           "",
		TotalLines:            0,
		TotalBytes:            0,
		OutputLines:           0,
		OutputBytes:           0,
		MaxLines:              0,
		MaxBytes:              0,
		Truncated:             false,
		LastLinePartial:       false,
		FirstLineExceedsLimit: false,
	}
}

func fetchTestSelection(startLine, totalLines int, userLimitedLines *int) fetchSelection {
	return fetchSelection{
		userLimitedLines: userLimitedLines,
		startLine:        startLine,
		totalLines:       totalLines,
	}
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
	return FetchInput{Timeout: nil, Offset: nil, Limit: nil, URL: rawURL, Format: format}
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

// fetchTestConn is a stub net.Conn used to observe the address that the
// validated dial hands to the base dialer without opening a real connection.
type fetchTestConn struct {
	remoteAddr net.Addr
}

func (conn fetchTestConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (conn fetchTestConn) Write(data []byte) (int, error)   { return len(data), nil }
func (conn fetchTestConn) Close() error                     { return nil }
func (conn fetchTestConn) LocalAddr() net.Addr              { return conn.remoteAddr }
func (conn fetchTestConn) RemoteAddr() net.Addr             { return conn.remoteAddr }
func (conn fetchTestConn) SetDeadline(time.Time) error      { return nil }
func (conn fetchTestConn) SetReadDeadline(time.Time) error  { return nil }
func (conn fetchTestConn) SetWriteDeadline(time.Time) error { return nil }
