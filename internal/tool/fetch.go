package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
	"github.com/omarluq/librecode/internal/units"
	"github.com/samber/oops"
)

const (
	fetchDefaultFormat  = "markdown"
	fetchFormatMarkdown = "markdown"
	fetchFormatText     = "text"
	fetchFormatHTML     = "html"

	fetchDefaultTimeout   = 30 * time.Second
	fetchMaxTimeout       = 120 * time.Second
	fetchMaxRedirects     = 10
	fetchReadLimitBytes   = 5 * units.MiB
	fetchTextWrapWidth    = 120
	fetchUserAgent        = "librecode/1.0 (+https://github.com/omarluq/librecode)"
	fetchAcceptHeader     = "text/html,application/xhtml+xml,application/json,text/plain,*/*;q=0.8"
	fetchNoiseSelector    = "script,style,noscript,template,nav,header,footer,aside,iframe,svg,form"
	fetchJSONContentType  = "application/json"
	fetchHTMLContentType  = "text/html"
	fetchXHTMLContentType = "application/xhtml+xml"
)

// FetchInput contains arguments for the fetch tool.
type FetchInput struct {
	Timeout *int   `json:"timeout,omitempty"`
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

	truncation := truncateFetchedContent(content)
	details := fetchDetails(input.URL, responseInfo, format, &truncation, title)

	return TextResult(fetchOutputText(&truncation), details), nil
}

func fetchDescription() string {
	return fmt.Sprintf(
		"Fetch an explicit HTTP(S) URL and return markdown, text, or HTML. "+
			"Default format is markdown. Response reads are capped at %s and output is truncated to %d lines or %s.",
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

	response, err := fetchTool.httpClientWithRedirectValidation(requestCtx).Do(request)
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

func (fetchTool *FetchTool) httpClientWithRedirectValidation(ctx context.Context) *http.Client {
	baseClient := fetchTool.httpClient()
	client := *baseClient
	baseCheckRedirect := baseClient.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if baseCheckRedirect != nil {
			if err := baseCheckRedirect(request, via); err != nil {
				return err
			}
		} else if len(via) >= fetchMaxRedirects {
			return oops.In("tool").Code("fetch_too_many_redirects").Errorf("fetch url stopped after 10 redirects")
		}

		return fetchTool.validatePublicFetchURL(ctx, request.URL)
	}

	return &client
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
		content = fetchedHTMLMarkdown(doc, finalURL)
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

func fetchedHTMLMarkdown(doc *goquery.Document, finalURL string) string {
	converter := md.NewConverter(fetchMarkdownDomain(finalURL), true, fetchMarkdownOptions(finalURL))
	markdown := converter.Convert(doc.Selection)

	return normalizeFetchedMarkdown(markdown)
}

func fetchMarkdownOptions(finalURL string) *md.Options {
	return &md.Options{
		GetAbsoluteURL: func(_ *goquery.Selection, rawURL, _ string) string {
			return resolveFetchURL(finalURL, rawURL)
		},
	}
}

func resolveFetchURL(baseURL, rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil || parsedURL.IsAbs() {
		return rawURL
	}

	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil {
		return rawURL
	}

	return parsedBaseURL.ResolveReference(parsedURL).String()
}

func fetchedHTMLBody(doc *goquery.Document) (string, error) {
	bodySelection := doc.Find("body").First()
	if bodySelection.Length() == 0 {
		htmlContent, err := doc.Html()
		if err != nil {
			return "", oops.In("tool").Code("fetch_render_html").Wrapf(err, "render fetched html")
		}

		return htmlContent, nil
	}

	bodyHTML, err := bodySelection.Html()
	if err != nil {
		return "", oops.In("tool").Code("fetch_render_html").Wrapf(err, "render fetched html")
	}

	return "<html>\n<body>\n" + strings.TrimSpace(bodyHTML) + "\n</body>\n</html>", nil
}

func fetchMarkdownDomain(finalURL string) string {
	parsedURL, err := url.Parse(finalURL)
	if err != nil {
		return ""
	}

	return parsedURL.Host
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
	return "```" + language + "\n" + content + "\n```"
}

func isFetchHTML(contentType string) bool {
	return contentType == fetchHTMLContentType ||
		contentType == fetchXHTMLContentType ||
		strings.HasSuffix(contentType, "+html")
}

func isFetchJSON(contentType string) bool {
	return contentType == fetchJSONContentType || strings.HasSuffix(contentType, "+json")
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
	}

	if title != "" {
		details["title"] = title
	}

	if truncation.Truncated {
		details[detailTruncation] = truncation
	}

	return details
}

func fetchOutputText(truncation *TruncationResult) string {
	if !truncation.Truncated {
		return truncation.Content
	}

	return fmt.Sprintf(
		"%s\n\n[Showing first %d lines of %d (%s limit).]",
		truncation.Content,
		truncation.OutputLines,
		truncation.TotalLines,
		FormatSize(truncation.MaxBytes),
	)
}
