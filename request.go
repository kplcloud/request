package request

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"golang.org/x/net/http2"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Decoder interface {
	Decode(data []byte, mediaType string, into interface{}) (interface{}, error)
}

type Request struct {
	client *http.Client

	verb string

	baseURL *url.URL
	method  string

	pathPrefix string
	subpath    string
	params     url.Values
	headers    http.Header
	timeout    time.Duration

	// output
	err  error
	body io.Reader

	ctx context.Context
}

type Result struct {
	body        []byte
	contentType string
	err         error
	statusCode  int
	headers     map[string][]string
	cookies     []*http.Cookie

	decoder Decoder
}

// Raw returns the raw result.
func (r Result) Raw() ([]byte, error) {
	return r.body, r.err
}

func (r Result) Into(obj interface{}) error {
	if r.err != nil {
		return r.Error()
	}

	if len(r.body) == 0 {
		return fmt.Errorf("0-length response")
	}

	mediaType, _, err := mime.ParseMediaType(r.contentType)
	if err != nil {
		return err
	}

	out, err := r.decoder.Decode(r.body, mediaType, &obj)
	if err != nil || out == obj {
		return err
	}

	return nil
}

// WasCreated updates the provided bool pointer to whether the server returned
// 201 created or a different response.
func (r Result) WasCreated(wasCreated *bool) Result {
	*wasCreated = r.statusCode == http.StatusCreated
	return r
}

func (r Result) Error() error {
	if r.err == nil || len(r.body) == 0 {
		return r.err
	}

	return r.err
}

func (r Result) HttpStatusCode() int {
	return r.statusCode
}

func (r Result) Headers() map[string][]string {
	return r.headers
}

func (r Result) Cookies() []*http.Cookie {
	return r.cookies
}

// StatusCode returns the HTTP status code of the request. (Only valid if no
// error was returned.)
func (r Result) StatusCode(statusCode *int) Result {
	*statusCode = r.statusCode
	return r
}

func NewRequest(baseUrl, verb string) *Request {
	dialer := &net.Dialer{
		Timeout:   time.Duration(30 * time.Second),
		KeepAlive: time.Duration(30 * time.Second),
	}

	var isHttps bool
	if strings.Index(baseUrl, "https") != -1 {
		isHttps = true
	}

	hostURL, err := url.Parse(baseUrl)
	if err != nil || hostURL.Scheme == "" || hostURL.Host == "" {
		scheme := "http://"
		if isHttps {
			scheme = "https://"
		}
		hostURL, _ = url.Parse(scheme + baseUrl)
	}

	pathPrefix := "/"
	if hostURL != nil {
		pathPrefix = path.Join(pathPrefix, hostURL.Path)
	}

	return &Request{
		headers: nil,
		baseURL: hostURL,
		client: &http.Client{
			Transport: &http.Transport{
				DialContext: dialer.DialContext,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: isHttps,
				},
			},
		},
		verb:       strings.ToUpper(verb),
		pathPrefix: pathPrefix,
	}
}

func (r *Request) HttpClient(client *http.Client) *Request {
	r.client = client
	return r
}

func (r *Request) Header(key string, values ...string) *Request {
	if r.headers == nil {
		r.headers = http.Header{}
	}
	r.headers.Del(key)
	for _, value := range values {
		r.headers.Add(key, value)
	}
	return r
}

func (r *Request) Timeout(d time.Duration) *Request {
	if r.err != nil {
		return r
	}
	r.timeout = d
	return r
}

func (r *Request) Context(ctx context.Context) *Request {
	r.ctx = ctx
	return r
}

func (r *Request) Prefix(segments ...string) *Request {
	if r.err != nil {
		return r
	}
	r.pathPrefix = path.Join(r.pathPrefix, path.Join(segments...))
	return r
}

func (r *Request) Suffix(segments ...string) *Request {
	if r.err != nil {
		return r
	}
	r.subpath = path.Join(r.subpath, path.Join(segments...))
	return r
}

func (r *Request) AbsPath(segments ...string) *Request {
	if r.err != nil {
		return r
	}
	r.pathPrefix = path.Join(r.baseURL.Path, path.Join(segments...))
	if len(segments) == 1 && (len(r.baseURL.Path) > 1 || len(segments[0]) > 1) && strings.HasSuffix(segments[0], "/") {
		// preserve any trailing slashes for legacy behavior
		r.pathPrefix += "/"
	}
	return r
}

func (r *Request) RequestURI(uri string) *Request {
	if r.err != nil {
		return r
	}
	locator, err := url.Parse(uri)
	if err != nil {
		r.err = err
		return r
	}
	r.pathPrefix = locator.Path
	if len(locator.Query()) > 0 {
		if r.params == nil {
			r.params = make(url.Values)
		}
		for k, v := range locator.Query() {
			r.params[k] = v
		}
	}
	return r
}

func (r *Request) Param(paramName, s string) *Request {
	if r.err != nil {
		return r
	}
	return r.setParam(paramName, s)
}

func (r *Request) setParam(paramName, value string) *Request {
	if r.params == nil {
		r.params = make(url.Values)
	}
	r.params[paramName] = append(r.params[paramName], value)
	return r
}

func (r *Request) Body(obj interface{}) *Request {
	if r.err != nil {
		return r
	}
	switch t := obj.(type) {
	case string:
		data, err := ioutil.ReadFile(t)
		if err != nil {
			r.err = err
			return r
		}
		r.body = bytes.NewReader(data)
	case []byte:
		r.body = bytes.NewReader(t)
	case io.Reader:
		r.body = t
	default:
		r.err = fmt.Errorf("unknown type used for body: %+v", obj)
	}
	return r
}

func (r *Request) URL() *url.URL {
	p := r.pathPrefix

	finalURL := &url.URL{}
	if r.baseURL != nil {
		*finalURL = *r.baseURL
	}
	finalURL.Path = p

	query := url.Values{}
	for key, values := range r.params {
		for _, value := range values {
			query.Add(key, value)
		}
	}

	// timeout is handled specially here.
	if r.timeout != 0 {
		query.Set("timeout", r.timeout.String())
	}
	finalURL.RawQuery = query.Encode()
	return finalURL
}

func (r *Request) Stream() (io.ReadCloser, error) {
	if r.err != nil {
		return nil, r.err
	}

	httpUrl := r.URL().String()
	req, err := http.NewRequest(r.verb, httpUrl, nil)
	if err != nil {
		return nil, err
	}
	if r.ctx != nil {
		req = req.WithContext(r.ctx)
	}
	req.Header = r.headers
	client := r.client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	switch {
	case (resp.StatusCode >= 200) && (resp.StatusCode < 300):
		return resp.Body, nil

	default:
		// ensure we close the body before returning the error
		defer func() {
			_ = resp.Body.Close()
		}()

		result := r.transformResponse(resp, req)
		err := result.Error()
		if err == nil {
			err = fmt.Errorf("%d while accessing %v: %s", result.statusCode, httpUrl, string(result.body))
		}
		return nil, err
	}
}

func (r *Request) Do() Result {
	var result Result
	err := r.request(func(req *http.Request, resp *http.Response) {
		result = r.transformResponse(resp, req)
	})
	if err != nil {
		return Result{err: err}
	}
	return result
}

func (r *Request) request(fn func(*http.Request, *http.Response)) error {
	if r.err != nil {
		return r.err
	}

	client := r.client
	if client == nil {
		client = http.DefaultClient
	}

	maxRetries := 10
	retries := 0
	for {
		httpUrl := r.URL().String()
		req, err := http.NewRequest(r.verb, httpUrl, r.body)
		if err != nil {
			return err
		}
		if r.ctx != nil {
			req = req.WithContext(r.ctx)
		}
		req.Header = r.headers

		resp, err := client.Do(req)
		if err != nil {
			if !IsConnectionReset(err) || r.verb != "GET" {
				return err
			}

			resp = &http.Response{
				StatusCode: http.StatusInternalServerError,
				Header:     http.Header{"Retry-After": []string{"1"}},
				Body:       ioutil.NopCloser(bytes.NewReader([]byte{})),
			}
		}

		done := func() bool {
			// Ensure the response body is fully read and closed
			// before we reconnect, so that we reuse the same TCP
			// connection.
			defer func() {
				const maxBodySlurpSize = 2 << 10
				if resp.ContentLength <= maxBodySlurpSize {
					_, _ = io.Copy(ioutil.Discard, &io.LimitedReader{R: resp.Body, N: maxBodySlurpSize})
				}
				_ = resp.Body.Close()
			}()

			retries++
			if _, wait := checkWait(resp); wait && retries < maxRetries {
				if seeker, ok := r.body.(io.Seeker); ok && r.body != nil {
					_, err := seeker.Seek(0, 0)
					if err != nil {
						fn(req, resp)
						return true
					}
				}
				return false
			}
			fn(req, resp)
			return true
		}()
		if done {
			return nil
		}
	}
}

func (r *Request) transformResponse(resp *http.Response, req *http.Request) Result {
	var body []byte
	if resp.Body != nil {
		data, err := ioutil.ReadAll(resp.Body)

		switch err.(type) {
		case nil:
			body = data
		case http2.StreamError:
			streamErr := fmt.Errorf("Stream error %#v when reading response body, may be caused by closed connection. Please retry.", err)
			return Result{
				err: streamErr,
			}
		default:
			unexpectedErr := fmt.Errorf("Unexpected error %#v when reading response body. Please retry.", err)
			return Result{
				err: unexpectedErr,
			}
		}
	}

	// verify the content type is accurate
	contentType := resp.Header.Get("Content-Type")
	decoder := NewDecode()

	switch {
	case resp.StatusCode == http.StatusSwitchingProtocols:
		// no-op, we've been upgraded
	case resp.StatusCode < http.StatusOK || resp.StatusCode > http.StatusPartialContent:
		return Result{
			body:        body,
			contentType: contentType,
			statusCode:  resp.StatusCode,
			decoder:     decoder,
			err:         r.transformUnstructuredResponseError(resp, req, body),
			headers:     resp.Header,
			cookies:     resp.Cookies(),
		}
	}

	return Result{
		body:        body,
		contentType: contentType,
		statusCode:  resp.StatusCode,
		decoder:     decoder,
		headers:     resp.Header,
		cookies:     resp.Cookies(),
	}
}

const maxUnstructuredResponseTextBytes = 2048

func (r *Request) transformUnstructuredResponseError(resp *http.Response, req *http.Request, body []byte) error {
	if body == nil && resp.Body != nil {
		if data, err := ioutil.ReadAll(&io.LimitedReader{R: resp.Body, N: maxUnstructuredResponseTextBytes}); err == nil {
			body = data
		}
	}
	retryAfter, _ := retryAfterSeconds(resp)
	return r.newUnstructuredResponseError(body, isTextResponse(resp), resp.StatusCode, req.Method, retryAfter)
}
func isTextResponse(resp *http.Response) bool {
	contentType := resp.Header.Get("Content-Type")
	if len(contentType) == 0 {
		return true
	}
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return strings.HasPrefix(media, "text/")
}
func (r *Request) newUnstructuredResponseError(body []byte, isTextResponse bool, statusCode int, method string, retryAfter int) error {
	// cap the amount of output we create
	if len(body) > maxUnstructuredResponseTextBytes {
		body = body[:maxUnstructuredResponseTextBytes]
	}

	message := "unknown"
	if isTextResponse {
		message = strings.TrimSpace(string(body))
	}
	return NewGenericServerResponse(statusCode, message)
}

type decode struct {
}

func NewDecode() Decoder {
	return &decode{}
}

func (c *decode) Decode(data []byte, mediaType string, into interface{}) (interface{}, error) {
	switch mediaType {
	case "application/json":
		if err := json.Unmarshal(data, &into); err != nil {
			return nil, err
		}
		return into, nil
	case "application/yaml":
		if err := yaml.Unmarshal(data, &into); err != nil {
			return nil, err
		}
		return into, nil
	case "application/xml", "text/xml":
		if err := xml.Unmarshal(data, &into); err != nil {
			return nil, err
		}
		return into, nil
	}
	return into, nil
}

func IsConnectionReset(err error) bool {
	if urlErr, ok := err.(*url.Error); ok {
		err = urlErr.Err
	}
	if opErr, ok := err.(*net.OpError); ok {
		err = opErr.Err
	}
	if osErr, ok := err.(*os.SyscallError); ok {
		err = osErr.Err
	}
	if errno, ok := err.(syscall.Errno); ok && errno == syscall.ECONNRESET {
		return true
	}
	return false
}

func checkWait(resp *http.Response) (int, bool) {
	switch r := resp.StatusCode; {
	// any 500 error code and 429 can trigger a wait
	case r == http.StatusTooManyRequests, r >= 500:
	default:
		return 0, false
	}
	i, ok := retryAfterSeconds(resp)
	return i, ok
}

func retryAfterSeconds(resp *http.Response) (int, bool) {
	if h := resp.Header.Get("Retry-After"); len(h) > 0 {
		if i, err := strconv.Atoi(h); err == nil {
			return i, true
		}
	}
	return 0, false
}

func NewGenericServerResponse(code int, serverMessage string) *StatusError {
	message := fmt.Sprintf("the server responded with the status code %d but did not return more information", code)
	switch code {
	case http.StatusConflict:
		message = "the server reported a conflict"
	case http.StatusNotFound:
		message = "the server could not find the requested resource"
	case http.StatusBadRequest:
		message = "the server rejected our request for an unknown reason"
	case http.StatusUnauthorized:
		message = "the server has asked for the client to provide credentials"
	case http.StatusForbidden:
		message = serverMessage
	case http.StatusNotAcceptable:
		message = serverMessage
	case http.StatusUnsupportedMediaType:
		message = serverMessage
	case http.StatusMethodNotAllowed:
		message = "the server does not allow this method on the requested resource"
	case http.StatusUnprocessableEntity:
		message = "the server rejected our request due to an error in our request"
	case http.StatusServiceUnavailable:
		message = "the server is currently unable to handle the request"
	case http.StatusGatewayTimeout:
		message = "the server was unable to return a response in the time allotted, but may still be processing the request"
	case http.StatusTooManyRequests:
		message = "the server has received too many requests and has asked us to try again later"
	default:
		if code >= 500 {
			message = fmt.Sprintf("an error on the server (%d) has prevented the request from succeeding", code)
		}
	}
	return &StatusError{
		Message: message,
	}
}

type StatusError struct {
	Message string
}

var _ error = &StatusError{}

// Error implements the Error interface.
func (e *StatusError) Error() string {
	return e.Message
}
