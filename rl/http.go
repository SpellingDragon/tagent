package rl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/rl/rl-architecture.md#http-api
//
// FeedbackPayload is the body of one feedback submission: the hex event key it
// targets, the decision or attempt it belongs to when the caller can name them, and
// the external conclusion to bind there.
//
// The type is a wire contract only and pulls in no HTTP dependency. Authentication
// runs at a single point before routing, so no endpoint, including a read-only one,
// has a bypass, and a caller credential stays out of any persisted record. A payload
// that cannot be resolved to an event is marked unbound, and a binding is never
// inherited from the most recent parent call.
type FeedbackPayload struct {
	// EventKey is the hex key of the event the feedback is bound to.
	EventKey string `json:"event_key"`
	// DecisionID names the decision the feedback belongs to; it is empty when the
	// caller names only an event key.
	DecisionID event.DecisionID `json:"decision_id,omitempty"`
	// AttemptID names the attempt the feedback belongs to; it is empty when the
	// feedback is bound at the decision boundary.
	AttemptID event.AttemptID `json:"attempt_id,omitempty"`
	// Conclusion is the external conclusion to bind.
	Conclusion string `json:"conclusion"`
	// Source names where the conclusion came from.
	Source string `json:"source,omitempty"`
}

// EndpointAllowlist is the declared host allowlist of the dynamic model endpoint: an
// exact host with a free port, a scheme restricted to http or https, and no userinfo
// or fragment in the URL.
//
// The allowlist is rechecked on every redirect hop, so a first hop to an allowed host
// that redirects outside the list is refused and no prompt is sent to the destination
// of that hop. An empty host list admits no destination at all.
type EndpointAllowlist struct {
	// Hosts lists the exact hosts allowed; the port is free.
	Hosts []string
	// Schemes lists the allowed URL schemes, each of http or https; an empty list
	// admits both of them and nothing else.
	Schemes []string
	// MaxRedirects is the number of redirect hops that are each rechecked; zero
	// follows no redirect at all.
	MaxRedirects int
}

// RequestLimits declares the single-point bounds every request is checked against,
// including a feedback submission.
//
// The limits are enforced in one place: a body over the byte bound is refused, the
// message count and the per-message byte bound are refused as well, and a value that
// would turn a bound off is a configuration error rather than a valid request.
type RequestLimits struct {
	// MaxBodyBytes bounds a request body.
	MaxBodyBytes int64
	// MaxMessages bounds the message count of a task submission.
	MaxMessages int
	// MaxMessageBytes bounds the content of a single message.
	MaxMessageBytes int64
	// MaxResponseBytes bounds a response body read back from the endpoint.
	MaxResponseBytes int64
	// Roles lists the roles a message may carry.
	Roles []string
}

// Feedback HTTP contract assertions pin every declared field: a renamed wire field
// or a changed field type stops the package from compiling.
var (
	_ = FeedbackPayload{
		EventKey:   "",
		DecisionID: "",
		AttemptID:  "",
		Conclusion: "",
		Source:     "",
	}

	_ = EndpointAllowlist{
		Hosts:        []string{},
		Schemes:      []string{},
		MaxRedirects: 0,
	}

	_ = RequestLimits{
		MaxBodyBytes:     0,
		MaxMessages:      0,
		MaxMessageBytes:  0,
		MaxResponseBytes: 0,
		Roles:            []string{},
	}
)

// DefaultMaxBodyBytes and DefaultMaxResponseBytes bound a request or response body
// that names no bound: a zero bound is enforced under these defaults, so no face in
// front of the model runs beside an unbounded body.
const (
	// DefaultMaxBodyBytes bounds a request body that names no bound.
	DefaultMaxBodyBytes int64 = 1 << 20
	// DefaultMaxResponseBytes bounds a response body that names no bound.
	DefaultMaxResponseBytes int64 = 4 << 20
)

// ErrRequestLimitsInvalid and the errors beside it name how a request bound, an
// endpoint allowlist or a listen address was refused, so a caller routes it by name.
var (
	// ErrRequestLimitsInvalid reports a bound that is negative: a negative bound
	// turns a limit off and is a configuration error, never a valid request.
	ErrRequestLimitsInvalid = errors.New("rl: a request bound is negative")
	// ErrBodyTooLarge reports a body read that passed the byte bound it was read
	// under. The read stops at the bound, so the exact size is not known and the
	// reason names the bound instead.
	ErrBodyTooLarge = errors.New("rl: the body passed the byte bound it was read under")
	// ErrEndpointURLInvalid reports a destination that does not parse as a URL.
	ErrEndpointURLInvalid = errors.New("rl: the endpoint URL does not parse")
	// ErrEndpointScheme reports a destination whose scheme is neither http nor
	// https, or is outside the declared scheme list.
	ErrEndpointScheme = errors.New("rl: the endpoint URL scheme is not allowed")
	// ErrEndpointUserInfo reports a destination carrying a credential in its URL.
	ErrEndpointUserInfo = errors.New("rl: the endpoint URL carries userinfo")
	// ErrEndpointFragment reports a destination carrying a fragment.
	ErrEndpointFragment = errors.New("rl: the endpoint URL carries a fragment")
	// ErrEndpointHost reports a destination whose host is not on the allowlist.
	ErrEndpointHost = errors.New("rl: the endpoint host is not on the allowlist")
	// ErrEndpointRedirect reports a redirect hop whose destination the allowlist
	// refuses: the hop is not followed and the destination receives no request.
	ErrEndpointRedirect = errors.New("rl: the endpoint redirect hop is not allowed")
	// ErrEndpointRedirectLimit reports a redirect chain longer than the declared
	// hop count.
	ErrEndpointRedirectLimit = errors.New("rl: the endpoint redirect chain exceeds the allowed hop count")
	// ErrEndpointStatus reports a destination that answered a status outside the
	// success range.
	ErrEndpointStatus = errors.New("rl: the endpoint answered a non-success status")
	// ErrInsecureListenAddr reports a listen address outside loopback beside no
	// request token: the face stays closed until a token, a loopback address or
	// the explicit unsafe switch is chosen.
	ErrInsecureListenAddr = errors.New("rl: refusing a non-loopback listen address without a request token (set " + RLTokenEnv + ", bind loopback, or pass the unsafe switch)")
)

// Normalize returns the bounds one request is enforced under: a zero bound takes
// its default and a negative bound is refused by name. Every check of the request
// reads the returned value, so a caller never sees two different bounds.
func (l RequestLimits) Normalize() (RequestLimits, error) {
	if l.MaxBodyBytes < 0 || l.MaxMessages < 0 || l.MaxMessageBytes < 0 || l.MaxResponseBytes < 0 {
		return RequestLimits{}, ErrRequestLimitsInvalid
	}
	if l.MaxBodyBytes == 0 {
		l.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if l.MaxResponseBytes == 0 {
		l.MaxResponseBytes = DefaultMaxResponseBytes
	}
	return l, nil
}

// readBounded reads at most the given byte bound and refuses a body that passes it,
// so no read of an unbounded stream is ever taken and the refusal carries the bound
// it was read under.
func readBounded(r io.Reader, max int64) ([]byte, error) {
	if max <= 0 {
		return nil, ErrRequestLimitsInvalid
	}
	body, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("%w: the %d byte bound is passed", ErrBodyTooLarge, max)
	}
	return body, nil
}

// Check judges one destination URL against the allowlist and returns the parsed
// form the request is sent to. A URL that does not parse, a scheme outside
// http/https or the declared list, userinfo, a fragment and a host outside the list
// are each refused by name.
func (a EndpointAllowlist) Check(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEndpointURLInvalid, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("%w: %q", ErrEndpointScheme, parsed.Scheme)
	}
	if len(a.Schemes) > 0 {
		declared := false
		for _, allowed := range a.Schemes {
			if strings.EqualFold(allowed, scheme) {
				declared = true
				break
			}
		}
		if !declared {
			return nil, fmt.Errorf("%w: %q", ErrEndpointScheme, parsed.Scheme)
		}
	}
	if parsed.User != nil {
		return nil, ErrEndpointUserInfo
	}
	if parsed.Fragment != "" {
		return nil, ErrEndpointFragment
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("%w: the URL names no host", ErrEndpointHost)
	}
	for _, allowed := range a.Hosts {
		if strings.EqualFold(allowed, host) {
			return parsed, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrEndpointHost, host)
}

// EndpointClient is the outbound face of one allowlisted dynamic endpoint: the
// request URL and every redirect hop are judged against the same allowlist before
// the hop is followed, and a response body is read under the declared byte bound.
type EndpointClient struct {
	allowlist EndpointAllowlist
	limits    RequestLimits
	client    *http.Client
}

// NewEndpointClient returns the outbound client of one allowlisted endpoint. An
// allowlist naming no host admits no destination, and a negative bound is refused
// by name.
func NewEndpointClient(allowlist EndpointAllowlist, limits RequestLimits) (*EndpointClient, error) {
	if len(allowlist.Hosts) == 0 {
		return nil, fmt.Errorf("%w: the allowlist names no host", ErrEndpointHost)
	}
	normalized, err := limits.Normalize()
	if err != nil {
		return nil, err
	}
	c := &EndpointClient{allowlist: allowlist, limits: normalized}
	c.client = &http.Client{CheckRedirect: c.checkRedirect}
	return c, nil
}

// checkRedirect judges one redirect hop before it is followed: the hop destination
// is rechecked against the allowlist and the chain is refused once it passes the
// declared hop count.
func (c *EndpointClient) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > c.allowlist.MaxRedirects {
		return fmt.Errorf("%w: hop %d", ErrEndpointRedirectLimit, len(via))
	}
	if _, err := c.allowlist.Check(req.URL.String()); err != nil {
		return fmt.Errorf("%w: hop %d: %w", ErrEndpointRedirect, len(via), err)
	}
	return nil
}

// Get fetches one allowlisted destination and returns the body it answered with.
// The destination URL is judged before the request is built, every redirect hop is
// judged before it is followed, and the body is read under the declared bound. A
// status outside the success range is refused without reading the body.
func (c *EndpointClient) Get(ctx context.Context, rawURL string) ([]byte, error) {
	if _, err := c.allowlist.Check(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEndpointURLInvalid, err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%w: status %d", ErrEndpointStatus, resp.StatusCode)
	}
	body, err := readBounded(resp.Body, c.limits.MaxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("the endpoint response body: %w", err)
	}
	return body, nil
}

// RLTokenEnv names the environment entry the RL request token is read from.
const RLTokenEnv = "TAGENT_RL_AUTH_TOKEN"

// RLTokenFromEnv reads the RL request token the environment names, trimmed of the
// surrounding space a shell leaves. An absent or empty entry reads as no token.
func RLTokenFromEnv() string {
	return strings.TrimSpace(os.Getenv(RLTokenEnv))
}

// ValidateListenAddr judges one listen address beside the token the face is
// configured with. A token admits every address; beside no token only loopback is
// admitted, unless the caller passes the explicit unsafe switch. The refusal names
// the three ways out.
func ValidateListenAddr(addr, token string, allowUnsafe bool) error {
	if strings.TrimSpace(token) != "" || allowUnsafe {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInsecureListenAddr, addr, err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInsecureListenAddr, addr)
}
