package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/PuerkitoBio/goquery"
	"github.com/omarluq/librecode/internal/units"
	"github.com/samber/oops"
)

const (
	fetchDefaultFormat  = "markdown"
	fetchFormatMarkdown = "markdown"
	fetchFormatText     = "text"
	fetchFormatHTML     = "html"

	fetchDefaultTimeout    = 30 * time.Second
	fetchMaxTimeout        = 120 * time.Second
	fetchMaxRedirects      = 10
	fetchReadLimitBytes    = 5 * units.MiB
	fetchTextWrapWidth     = 120
	fetchUserAgent         = "librecode/1.0 (+https://github.com/omarluq/librecode)"
	fetchAcceptHeader      = "text/html,application/xhtml+xml,application/json,text/plain,*/*;q=0.8"
	fetchNoiseSelector     = "script,style,noscript,template,nav,header,footer,aside,iframe,svg,form"
	fetchJSONContentType   = "application/json"
	fetchHTMLContentType   = "text/html"
	fetchXHTMLContentType  = "application/xhtml+xml"
	fetchRenderHTMLContext = "render fetched html"
	fetchMinCodeFenceWidth = 3
)

// FetchInput contains arguments for the fetch tool.
type FetchInput struct {
	Timeout *int   `json:"timeout,omitempty"`
	Offset  *int   `json:"offset,omitempty"`
	Limit   *int   `json:"limit,omitempty"`
	URL     string `json:"url"`
	Format  string `json:"format,omitempty" jsonschema:"enum=markdown,enum=text,enum=html"`
}

// FetchTool fetches explicit HTTP(S) URLs.
type FetchTool struct {
	client               *http.Client
	lookupIPAddrs        func(context.Context, string) ([]net.IPAddr, error)
	allowPrivateNetworks bool
}

// NewFetchTool creates the fetch tool.
func NewFetchTool() *FetchTool {
	return &FetchTool{
		client:               http.DefaultClient,
		lookupIPAddrs:        nil,
		allowPrivateNetworks: false,
	}
}

// Definition returns fetch tool metadata.
func (fetchTool *FetchTool) Definition() Definition {
	return Definition{
		Schema:        inputSchemaForName(NameFetch),
		Name:          NameFetch,
		Label:         "fetch",
		Description:   fetchDescription(),
		PromptSnippet: "Fetch an explicit HTTP(S) URL",
		PromptGuidelines: []string{
			"Use fetch only for explicit URLs that are relevant to the user's task.",
			"Fetch does not search the web; provide the exact http:// or https:// URL to read.",
		},
		ReadOnly: true,
	}
}

// Execute runs the fetch tool.
func (fetchTool *FetchTool) Execute(ctx context.Context, input Arguments) (Result, error) {
	var args FetchInput

	err := decodeInput(input, &args)
	if err != nil {
		return emptyToolResult(), err
	}

	return fetchTool.Fetch(ctx, args)
}

// Fetch fetches one explicit HTTP(S) URL.
func (fetchTool *FetchTool) Fetch(ctx context.Context, input FetchInput) (Result, error) {
	requestURL, format, timeout, err := validateFetchInput(input)
	if err != nil {
		return emptyToolResult(), err
	}

	body, responseInfo, err := fetchTool.fetchURL(ctx, requestURL, timeout)
	if err != nil {
		return emptyToolResult(), err
	}

	content, title, err := formatFetchedContent(body, responseInfo.contentType, responseInfo.finalURL, format)
	if err != nil {
		return emptyToolResult(), err
	}

	selectedContent, selection, err := selectFetchedContent(content, input.Offset, input.Limit)
	if err != nil {
		return emptyToolResult(), err
	}

	truncation := truncateFetchedContent(selectedContent)
	details := fetchDetails(input.URL, responseInfo, format, &truncation, title, selection)

	return TextResult(fetchOutputText(&truncation, selection, responseInfo.readLimitReached), details), nil
}

func fetchDescription() string {
	return fmt.Sprintf(
		"Fetch an explicit HTTP(S) URL and return markdown, text, or HTML. "+
			"Default format is markdown. Response reads are capped at %s and output is truncated to %d lines or %s. "+
			"Use offset/limit for large fetched output.",
		FormatSize(fetchReadLimitBytes),
		DefaultMaxLines,
		FormatSize(DefaultMaxBytes),
	)
}

func validateFetchInput(input FetchInput) (*url.URL, string, time.Duration, error) {
	requestURL, err := parseFetchURL(input.URL)
	if err != nil {
		return nil, "", 0, err
	}

	format, err := normalizeFetchFormat(input.Format)
	if err != nil {
		return nil, "", 0, err
	}

	timeout, err := normalizeFetchTimeout(input.Timeout)
	if err != nil {
		return nil, "", 0, err
	}

	if err := validateFetchPagination(input); err != nil {
		return nil, "", 0, err
	}

	return requestURL, format, timeout, nil
}

func parseFetchURL(rawURL string) (*url.URL, error) {
	trimmedURL := strings.TrimSpace(rawURL)
	if trimmedURL == "" {
		return nil, oops.In("tool").Code("fetch_url_required").Errorf("fetch url is required")
	}

	parsedURL, err := url.ParseRequestURI(trimmedURL)
	if err != nil {
		return nil, oops.In("tool").Code("fetch_invalid_url").Wrapf(err, "parse fetch url")
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, oops.In("tool").Code("fetch_unsupported_scheme").Errorf("fetch url must use http or https")
	}

	if parsedURL.Host == "" {
		return nil, oops.In("tool").Code("fetch_missing_host").Errorf("fetch url host is required")
	}

	return parsedURL, nil
}

func normalizeFetchFormat(format string) (string, error) {
	normalized := strings.TrimSpace(strings.ToLower(format))
	switch normalized {
	case "":
		return fetchDefaultFormat, nil
	case fetchFormatMarkdown, fetchFormatText, fetchFormatHTML:
		return normalized, nil
	default:
		return "", oops.In("tool").Code("fetch_invalid_format").Errorf("fetch format must be markdown, text, or html")
	}
}

func normalizeFetchTimeout(timeout *int) (time.Duration, error) {
	if timeout == nil {
		return fetchDefaultTimeout, nil
	}

	if *timeout < 1 {
		return 0, oops.In("tool").Code("fetch_invalid_timeout").Errorf("fetch timeout must be greater than zero")
	}

	maxTimeoutSeconds := int(fetchMaxTimeout / time.Second)
	if *timeout > maxTimeoutSeconds {
		return fetchMaxTimeout, nil
	}

	return time.Duration(*timeout) * time.Second, nil
}

func validateFetchPagination(input FetchInput) error {
	if input.Offset != nil && *input.Offset < 1 {
		return oops.In("tool").Code("fetch_invalid_offset").Errorf("fetch offset must be greater than zero")
	}

	if input.Limit != nil && *input.Limit < 1 {
		return oops.In("tool").Code("fetch_invalid_limit").Errorf("fetch limit must be greater than zero")
	}

	return nil
}

type fetchResponseInfo struct {
	finalURL         string
	contentType      string
	status           string
	statusCode       int
	bytesRead        int
	readLimitReached bool
}

func (fetchTool *FetchTool) fetchURL(
	ctx context.Context,
	requestURL *url.URL,
	timeout time.Duration,
) ([]byte, fetchResponseInfo, error) {
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := fetchTool.validatePublicFetchURL(requestCtx, requestURL); err != nil {
		return nil, fetchResponseInfo{}, err
	}

	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, requestURL.String(), http.NoBody)
	if err != nil {
		return nil, fetchResponseInfo{}, oops.In("tool").Code("fetch_build_request").Wrapf(err, "build fetch request")
	}

	setFetchHeaders(request)

	client, closeIdleConnections := fetchTool.httpClientWithRedirectValidation(requestCtx)
	defer closeIdleConnections()

	response, err := client.Do(request)
	if err != nil {
		return nil, fetchResponseInfo{}, oops.In("tool").Code("fetch_request").Wrapf(err, "fetch url")
	}

	body, limitReached, readErr := readLimitedFetchBody(response.Body)
	closeErr := response.Body.Close()

	if readErr != nil {
		return nil, fetchResponseInfo{}, readErr
	}

	if closeErr != nil {
		return nil, fetchResponseInfo{}, oops.In("tool").
			Code("fetch_close_body").
			Wrapf(closeErr, "close fetch response")
	}

	contentType := normalizedFetchContentType(response.Header.Get("Content-Type"))
	info := fetchResponseInfo{
		finalURL:         response.Request.URL.String(),
		contentType:      contentType,
		status:           response.Status,
		statusCode:       response.StatusCode,
		bytesRead:        len(body),
		readLimitReached: limitReached,
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, info, oops.In("tool").
			Code("fetch_http_status").
			With("status", response.StatusCode).
			Errorf("fetch url returned %s", response.Status)
	}

	if !utf8.Valid(body) {
		return nil, info, oops.In("tool").Code("fetch_invalid_utf8").Errorf("fetch response is not valid UTF-8")
	}

	return body, info, nil
}

func (fetchTool *FetchTool) httpClient() *http.Client {
	if fetchTool.client != nil {
		return fetchTool.client
	}

	return http.DefaultClient
}

func (fetchTool *FetchTool) httpClientWithRedirectValidation(
	ctx context.Context,
) (client *http.Client, closeIdleConnections func()) {
	baseClient := fetchTool.httpClient()
	clonedClient := *baseClient

	transport, closeIdleConnections := fetchTool.transportWithNetworkValidation(baseClient.Transport)
	clonedClient.Transport = transport

	baseCheckRedirect := baseClient.CheckRedirect
	clonedClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if baseCheckRedirect != nil {
			if err := baseCheckRedirect(request, via); err != nil {
				return err
			}
		} else if len(via) >= fetchMaxRedirects {
			return oops.In("tool").Code("fetch_too_many_redirects").Errorf("fetch url stopped after 10 redirects")
		}

		return fetchTool.validatePublicFetchURL(ctx, request.URL)
	}

	return &clonedClient, closeIdleConnections
}

func (fetchTool *FetchTool) transportWithNetworkValidation(
	baseTransport http.RoundTripper,
) (roundTripper http.RoundTripper, closeIdleConnections func()) {
	if fetchTool.allowPrivateNetworks {
		return baseTransport, func() {}
	}

	transport, ok := cloneFetchHTTPTransport(baseTransport)
	if !ok {
		return baseTransport, func() {}
	}

	transport.Proxy = nil
	transport.DialContext = validatingFetchDialContext(fetchDialContext(transport))

	if transport.DialTLSContext != nil {
		transport.DialTLSContext = validatingFetchDialContext(transport.DialTLSContext)
	}

	return transport, transport.CloseIdleConnections
}

func cloneFetchHTTPTransport(baseTransport http.RoundTripper) (*http.Transport, bool) {
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}

	transport, ok := baseTransport.(*http.Transport)
	if !ok {
		return nil, false
	}

	return transport.Clone(), true
}

func fetchDialContext(transport *http.Transport) func(context.Context, string, string) (net.Conn, error) {
	if transport.DialContext != nil {
		return transport.DialContext
	}

	dialer := &net.Dialer{}

	return dialer.DialContext
}

func validatingFetchDialContext(
	dialContext func(context.Context, string, string) (net.Conn, error),
) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return validateFetchDialedConnection(dialContext(ctx, network, address))
	}
}

func validateFetchDialedConnection(conn net.Conn, dialErr error) (net.Conn, error) {
	if dialErr != nil {
		return nil, dialErr
	}

	if conn == nil {
		return nil, oops.In("tool").Code("fetch_nil_connection").Errorf("fetch dial returned nil connection")
	}

	if err := validatePublicFetchRemoteAddr(conn.RemoteAddr()); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			return nil, errors.Join(
				err,
				oops.In("tool").Code("fetch_close_rejected_connection").Wrapf(
					closeErr,
					"close rejected fetch connection",
				),
			)
		}

		return nil, err
	}

	return conn, nil
}

func (fetchTool *FetchTool) validatePublicFetchURL(ctx context.Context, requestURL *url.URL) error {
	if fetchTool.allowPrivateNetworks {
		return nil
	}

	host := normalizedFetchHost(requestURL.Hostname())
	if host == "" {
		return oops.In("tool").Code("fetch_missing_host").Errorf("fetch url host is required")
	}

	if isLocalhostFetchHost(host) {
		return privateFetchNetworkError()
	}

	if ip := parseFetchHostIP(host); ip != nil {
		return validatePublicFetchIP(ip)
	}

	addrs, err := fetchTool.lookupFetchIPAddrs(ctx, host)
	if err != nil {
		return err
	}

	if len(addrs) == 0 {
		return oops.In("tool").Code("fetch_resolve_host").Errorf("resolve fetch url host returned no addresses")
	}

	for _, addr := range addrs {
		if err := validatePublicFetchIP(addr.IP); err != nil {
			return err
		}
	}

	return nil
}

func (fetchTool *FetchTool) lookupFetchIPAddrs(ctx context.Context, host string) ([]net.IPAddr, error) {
	if fetchTool.lookupIPAddrs != nil {
		return fetchTool.lookupIPAddrs(ctx, host)
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, oops.In("tool").Code("fetch_resolve_host").Wrapf(err, "resolve fetch url host")
	}

	return addrs, nil
}

func normalizedFetchHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func isLocalhostFetchHost(host string) bool {
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

func parseFetchHostIP(host string) net.IP {
	if zoneIndex := strings.LastIndexByte(host, '%'); zoneIndex >= 0 {
		host = host[:zoneIndex]
	}

	return net.ParseIP(host)
}

func validatePublicFetchIP(ipAddress net.IP) error {
	if ipAddress == nil || isPrivateFetchIP(ipAddress) {
		return privateFetchNetworkError()
	}

	return nil
}

func validatePublicFetchRemoteAddr(remoteAddr net.Addr) error {
	ipAddress := fetchRemoteAddrIP(remoteAddr)
	if ipAddress == nil {
		return oops.In("tool").Code("fetch_invalid_remote_address").Errorf("fetch remote address is not an IP")
	}

	return validatePublicFetchIP(ipAddress)
}

func fetchRemoteAddrIP(remoteAddr net.Addr) net.IP {
	if remoteAddr == nil {
		return nil
	}

	switch addr := remoteAddr.(type) {
	case *net.TCPAddr:
		return addr.IP
	case *net.UDPAddr:
		return addr.IP
	case *net.IPAddr:
		return addr.IP
	}

	host, _, err := net.SplitHostPort(remoteAddr.String())
	if err != nil {
		host = remoteAddr.String()
	}

	return parseFetchHostIP(normalizedFetchHost(host))
}

func isPrivateFetchIP(ipAddress net.IP) bool {
	return ipAddress.IsLoopback() ||
		ipAddress.IsPrivate() ||
		ipAddress.IsLinkLocalUnicast() ||
		ipAddress.IsLinkLocalMulticast() ||
		ipAddress.IsUnspecified() ||
		ipAddress.IsMulticast()
}

func privateFetchNetworkError() error {
	return oops.In("tool").
		Code("fetch_private_network").
		Errorf("fetch url must not target private or local networks")
}

func setFetchHeaders(request *http.Request) {
	request.Header.Set("User-Agent", fetchUserAgent)
	request.Header.Set("Accept", fetchAcceptHeader)
	request.Header.Set("Accept-Language", "en-US,en;q=0.9")
}

func readLimitedFetchBody(body io.Reader) (data []byte, limitReached bool, err error) {
	limitedBody := io.LimitReader(body, int64(fetchReadLimitBytes)+1)

	data, err = io.ReadAll(limitedBody)
	if err != nil {
		return nil, false, oops.In("tool").Code("fetch_read_body").Wrapf(err, "read fetch response")
	}

	if len(data) <= fetchReadLimitBytes {
		return data, false, nil
	}

	return data[:fetchReadLimitBytes], true, nil
}

func normalizedFetchContentType(header string) string {
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil || mediaType == "" {
		return strings.ToLower(strings.TrimSpace(header))
	}

	return strings.ToLower(mediaType)
}

func formatFetchedContent(body []byte, contentType, finalURL, format string) (content, title string, err error) {
	bodyText := string(body)
	if isFetchHTML(contentType) {
		return formatFetchedHTML(bodyText, finalURL, format)
	}

	return formatFetchedNonHTML(bodyText, contentType, format)
}

func formatFetchedHTML(bodyText, finalURL, format string) (content, title string, err error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyText))
	if err != nil {
		return "", "", oops.In("tool").Code("fetch_parse_html").Wrapf(err, "parse fetched html")
	}

	cleanFetchDocument(doc)
	title = strings.TrimSpace(doc.Find("title").First().Text())

	switch format {
	case fetchFormatMarkdown:
		content, err = fetchedHTMLMarkdown(doc, finalURL)
	case fetchFormatText:
		content = wrapFetchedText(doc.Find("body").Text())
	case fetchFormatHTML:
		content, err = fetchedHTMLBody(doc)
	default:
		err = oops.In("tool").Code("fetch_invalid_format").
			Errorf("fetch format must be markdown, text, or html")
	}

	return content, title, err
}

func cleanFetchDocument(doc *goquery.Document) {
	doc.Find(fetchNoiseSelector).Remove()
}

func fetchedHTMLMarkdown(doc *goquery.Document, finalURL string) (string, error) {
	htmlContent, _, err := fetchedDocumentHTML(doc)
	if err != nil {
		return "", err
	}

	markdown, err := htmltomarkdown.ConvertString(htmlContent, converter.WithDomain(finalURL))
	if err != nil {
		return "", oops.In("tool").Code("fetch_convert_markdown").Wrapf(err, "convert fetched html to markdown")
	}

	return normalizeFetchedMarkdown(markdown), nil
}

func fetchedDocumentHTML(doc *goquery.Document) (htmlContent string, foundBody bool, err error) {
	bodySelection := doc.Find("body").First()
	if bodySelection.Length() > 0 {
		htmlContent, err = bodySelection.Html()
		if err != nil {
			return "", false, oops.In("tool").Code("fetch_render_html").Wrapf(err, fetchRenderHTMLContext)
		}

		return htmlContent, true, nil
	}

	htmlContent, err = doc.Html()
	if err != nil {
		return "", false, oops.In("tool").Code("fetch_render_html").Wrapf(err, fetchRenderHTMLContext)
	}

	return htmlContent, false, nil
}

func fetchedHTMLBody(doc *goquery.Document) (string, error) {
	htmlContent, foundBody, err := fetchedDocumentHTML(doc)
	if err != nil {
		return "", err
	}

	if !foundBody {
		return htmlContent, nil
	}

	return "<html>\n<body>\n" + strings.TrimSpace(htmlContent) + "\n</body>\n</html>", nil
}

func normalizeFetchedMarkdown(markdown string) string {
	lines := strings.Split(strings.TrimSpace(markdown), "\n")
	for index, line := range lines {
		lines[index] = strings.TrimRight(line, " \t")
	}

	return collapseBlankLines(strings.Join(lines, "\n"))
}

func collapseBlankLines(content string) string {
	var builder strings.Builder

	blankCount := 0

	for line := range strings.SplitSeq(content, "\n") {
		if strings.TrimSpace(line) == "" {
			blankCount++
			if blankCount > 1 {
				continue
			}
		} else {
			blankCount = 0
		}

		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}

		builder.WriteString(line)
	}

	return strings.TrimSpace(builder.String())
}

func wrapFetchedText(content string) string {
	words := strings.Fields(content)
	if len(words) == 0 {
		return ""
	}

	var builder strings.Builder

	lineLength := 0

	for _, word := range words {
		if lineLength > 0 && lineLength+1+len(word) > fetchTextWrapWidth {
			builder.WriteByte('\n')

			lineLength = 0
		}

		if lineLength > 0 {
			builder.WriteByte(' ')

			lineLength++
		}

		builder.WriteString(word)

		lineLength += len(word)
	}

	return builder.String()
}

func formatFetchedNonHTML(bodyText, contentType, format string) (content, title string, err error) {
	formattedText, fenceLanguage := readableFetchedNonHTML(bodyText, contentType)

	if format == fetchFormatMarkdown {
		return fencedCodeBlock(fenceLanguage, formattedText), "", nil
	}

	return formattedText, "", nil
}

func readableFetchedNonHTML(bodyText, contentType string) (content, fenceLanguage string) {
	if isFetchJSON(contentType) || json.Valid([]byte(bodyText)) {
		var decoded any
		if err := json.Unmarshal([]byte(bodyText), &decoded); err == nil {
			var buffer bytes.Buffer

			encoder := json.NewEncoder(&buffer)
			encoder.SetIndent("", "  ")
			encoder.SetEscapeHTML(false)

			if err := encoder.Encode(decoded); err == nil {
				return strings.TrimRight(buffer.String(), "\n"), "json"
			}
		}
	}

	return bodyText, "text"
}

func fencedCodeBlock(language, content string) string {
	fence := strings.Repeat("`", max(fetchMinCodeFenceWidth, longestBacktickRun(content)+1))

	return fence + language + "\n" + content + "\n" + fence
}

func longestBacktickRun(content string) int {
	longest := 0
	current := 0

	for _, char := range content {
		if char == '`' {
			current++
			longest = max(longest, current)

			continue
		}

		current = 0
	}

	return longest
}

func isFetchHTML(contentType string) bool {
	return contentType == fetchHTMLContentType ||
		contentType == fetchXHTMLContentType ||
		strings.HasSuffix(contentType, "+html")
}

func isFetchJSON(contentType string) bool {
	return contentType == fetchJSONContentType || strings.HasSuffix(contentType, "+json")
}

type fetchSelection struct {
	userLimitedLines *int
	startLine        int
	totalLines       int
}

func selectFetchedContent(content string, offset, limit *int) (string, fetchSelection, error) {
	lines := strings.Split(content, "\n")

	startLine := 0
	if offset != nil {
		startLine = *offset - 1
	}

	if startLine >= len(lines) {
		return "", fetchSelection{}, oops.
			In("tool").
			Code("fetch_offset_beyond_output").
			With("offset", offset).
			With("total_lines", len(lines)).
			Errorf("fetch offset is beyond fetched output")
	}

	selectedContent, userLimitedLines := selectFetchLines(lines, startLine, limit)

	return selectedContent, fetchSelection{
		startLine:        startLine,
		totalLines:       len(lines),
		userLimitedLines: userLimitedLines,
	}, nil
}

func selectFetchLines(lines []string, startLine int, limit *int) (selectedContent string, userLimitedLines *int) {
	if limit == nil {
		return strings.Join(lines[startLine:], "\n"), nil
	}

	endLine := min(startLine+*limit, len(lines))
	selectedLines := endLine - startLine

	return strings.Join(lines[startLine:endLine], "\n"), &selectedLines
}

func truncateFetchedContent(content string) TruncationResult {
	truncation := TruncateHead(content, TruncationOptions{MaxLines: 0, MaxBytes: 0})
	if !truncation.FirstLineExceedsLimit {
		return truncation
	}

	truncation.Content = validUTF8Prefix(content, truncation.MaxBytes)
	truncation.OutputLines = 1
	truncation.OutputBytes = len([]byte(truncation.Content))
	truncation.LastLinePartial = true

	return truncation
}

func validUTF8Prefix(content string, maxBytes int) string {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content
	}

	prefix := content[:maxBytes]
	for !utf8.ValidString(prefix) && prefix != "" {
		prefix = prefix[:len(prefix)-1]
	}

	return prefix
}

func fetchDetails(
	requestedURL string,
	responseInfo fetchResponseInfo,
	format string,
	truncation *TruncationResult,
	title string,
	selection fetchSelection,
) map[string]any {
	details := map[string]any{
		"url":                requestedURL,
		"final_url":          responseInfo.finalURL,
		"status":             responseInfo.statusCode,
		"status_text":        responseInfo.status,
		"content_type":       responseInfo.contentType,
		"format":             format,
		"truncated":          truncation.Truncated || responseInfo.readLimitReached,
		"bytes_read":         responseInfo.bytesRead,
		"read_limit_reached": responseInfo.readLimitReached,
		"offset":             selection.startLine + 1,
		"total_lines":        selection.totalLines,
	}

	if selection.userLimitedLines != nil {
		details["limit"] = *selection.userLimitedLines
	}

	if title != "" {
		details["title"] = title
	}

	if truncation.Truncated {
		details[detailTruncation] = truncation
	}

	return details
}

func fetchOutputText(truncation *TruncationResult, selection fetchSelection, readLimitReached bool) string {
	if truncation.Truncated {
		return truncatedFetchOutput(truncation, selection)
	}

	if selection.userLimitedLines != nil && selection.startLine+*selection.userLimitedLines < selection.totalLines {
		remainingLines := selection.totalLines - (selection.startLine + *selection.userLimitedLines)
		nextOffset := selection.startLine + *selection.userLimitedLines + 1

		return fmt.Sprintf(
			"%s\n\n[%d more lines in fetched output. Use offset=%d to continue.]",
			truncation.Content,
			remainingLines,
			nextOffset,
		)
	}

	if readLimitReached {
		return truncation.Content + "\n\n[Response read limit reached; remaining remote content was not downloaded.]"
	}

	return truncation.Content
}

func truncatedFetchOutput(truncation *TruncationResult, selection fetchSelection) string {
	startLineDisplay := selection.startLine + 1
	endLineDisplay := startLineDisplay + truncation.OutputLines - 1
	nextOffset := endLineDisplay + 1

	if truncation.TruncatedBy == TruncatedByLines {
		return fmt.Sprintf(
			"%s\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]",
			truncation.Content,
			startLineDisplay,
			endLineDisplay,
			selection.totalLines,
			nextOffset,
		)
	}

	return fmt.Sprintf(
		"%s\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]",
		truncation.Content,
		startLineDisplay,
		endLineDisplay,
		selection.totalLines,
		FormatSize(truncation.MaxBytes),
		nextOffset,
	)
}
