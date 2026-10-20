// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// facePortEnv, faceDefaultPort and faceTokenEnv are the interface constants of the
// host face: the port the endpoints serve on, the default when the environment
// names none, and the request credential entry the inbound channel reads under the
// same name.
const (
	facePortEnv     = "TAGENT_HTTP_PORT"
	faceDefaultPort = "8089"
	faceTokenEnv    = "TAGENT_RL_AUTH_TOKEN"
)

// faceBodyLimitByte and faceShutdownGrace bound one request and one close: the body
// an injection may carry, and the time the face waits for in-flight requests.
const (
	faceBodyLimitByte = 1 << 20
	faceShutdownGrace = 5 * time.Second
)

// ErrFaceUnauthenticated reports an injection request whose subject did not
// authenticate: no credential is configured for the face and the peer is not the
// loopback address, or a configured credential was missing or unmatched.
var ErrFaceUnauthenticated = errors.New("wechat-bot: the injection request is not authenticated")

// ErrFaceNoActiveChat reports an injection request arriving beside no chat
// delivery: an injection carries no chat identity of its own, so it is admitted at
// the chat that last delivered.
var ErrFaceNoActiveChat = errors.New("wechat-bot: no chat delivered yet, so the injection has no session to drive")

// ErrFaceEmptyInjection reports an injection request whose messages carry no user
// content: a turn reads user input only, so the request is refused by name.
var ErrFaceEmptyInjection = errors.New("wechat-bot: the injection carries no user message")

// faceDrive is the narrow host face the endpoints drive: one delivery admitted at
// the chat that last delivered, and that chat's identity.
type faceDrive interface {
	Drive(ctx context.Context, in inboundMessage) (event.Address, error)
	mostRecentChat() string
}

// taskInjection is the body an inbound channel posts to the injection endpoint:
// the messages the turn reads and the lineage the channel declares for it.
type taskInjection struct {
	Messages      []injectionMessage `json:"messages"`
	TriggerSource string             `json:"trigger_source"`
}

// injectionMessage is one message of an injection: the role it speaks for and its
// content.
type injectionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// userText reports the user content the injection carries: the last non-empty
// user message is the one a turn reads, and an injection beside one answers no
// text.
func (in taskInjection) userText() string {
	for i := len(in.Messages) - 1; i >= 0; i-- {
		msg := in.Messages[i]
		if msg.Role != string(tagentmodel.RoleUser) {
			continue
		}
		if text := strings.TrimSpace(msg.Content); text != "" {
			return msg.Content
		}
	}
	return ""
}

// taskFace serves the host's two HTTP endpoints: the liveness probe a supervisor
// polls and the injection endpoint an inbound channel posts a task to. The face
// drives the run through the narrow face it holds and adds no outbound path of its
// own.
type taskFace struct {
	ctx   context.Context
	drive faceDrive
	token string
}

// newTaskFace returns the host face over one drive face beside the request
// credential it accepts; an empty credential leaves the loopback peer as the only
// admitted subject.
func newTaskFace(ctx context.Context, drive faceDrive, token string) *taskFace {
	return &taskFace{ctx: ctx, drive: drive, token: strings.TrimSpace(token)}
}

// Handler returns the endpoints of the face: GET /healthz answers the liveness
// probe every supervisor polls, and POST /task admits one injection and answers
// 202 before the turn runs.
func (f *taskFace) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	})
	mux.HandleFunc("POST /task", f.inject)
	return mux
}

// inject admits one injection and answers before its turn runs: the body's user
// content is driven at the chat that last delivered, in the process context so a
// shutdown still reaches the turn. A refused subject answers 401, a body beside no
// user content 400, and a face beside no delivered chat 503.
func (f *taskFace) inject(w http.ResponseWriter, r *http.Request) {
	if err := f.authorize(r); err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="tagent"`)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, faceBodyLimitByte))
	if err != nil {
		http.Error(w, fmt.Sprintf("read the injection body: %v", err), http.StatusBadRequest)
		return
	}
	var injection taskInjection
	if err := json.Unmarshal(body, &injection); err != nil {
		http.Error(w, fmt.Sprintf("decode the injection body: %v", err), http.StatusBadRequest)
		return
	}
	text := injection.userText()
	if text == "" {
		http.Error(w, ErrFaceEmptyInjection.Error(), http.StatusBadRequest)
		return
	}
	chat := f.drive.mostRecentChat()
	if chat == "" {
		http.Error(w, ErrFaceNoActiveChat.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	go func() {
		if _, err := f.drive.Drive(f.ctx, inboundMessage{ChatID: chat, UserName: chat, Text: text}); err != nil {
			log.Warnf("[host] injected task failed (chat=%s): %v", chat, err)
		}
	}()
}

// authorize judges one injection request: a configured credential must arrive as a
// bearer token and match in constant time, and a face beside no credential admits
// the loopback peer only.
func (f *taskFace) authorize(r *http.Request) error {
	if f.token != "" {
		scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			return ErrFaceUnauthenticated
		}
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(f.token)) != 1 {
			return ErrFaceUnauthenticated
		}
		return nil
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ErrFaceUnauthenticated
	}
	if !net.ParseIP(host).IsLoopback() {
		return ErrFaceUnauthenticated
	}
	return nil
}

// Listen opens the endpoint address of the face: the loopback interface on the
// port the host environment names, and the interface default beside no port.
func (f *taskFace) Listen() (net.Listener, error) {
	return net.Listen("tcp", faceAddr(os.Getenv(facePortEnv)))
}

// Serve answers the endpoints on the listener until the process context ends, then
// stops accepting and waits for the in-flight handlers.
func (f *taskFace) Serve(listener net.Listener) error {
	server := &http.Server{Handler: f.Handler()}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	select {
	case err := <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-f.ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), faceShutdownGrace)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	<-served
	return nil
}

// faceAddr reports the listen address of the face: the loopback interface on the
// named port, and on the interface default when the value names no port number.
func faceAddr(port string) string {
	if _, err := strconv.Atoi(port); err != nil {
		port = faceDefaultPort
	}
	return net.JoinHostPort("127.0.0.1", port)
}
