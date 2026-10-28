// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// The loopback endpoints of the host: the liveness probe a supervisor polls, the
// task endpoint an inbound channel posts to, and the reload endpoint a supervisor
// posts to after it rewrites the configuration source. Every endpoint authorizes
// before it reads a body, and a face that is switched off names that state in its
// reason instead of answering a request it never accepted.
package assembly

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
	"github.com/SpellingDragon/tagent/api/model"
)

const (
	// hostFacePortEnv names the environment variable holding the endpoint port.
	hostFacePortEnv = "TAGENT_HTTP_PORT"
	// hostFaceDefaultPort is the endpoint port beside no configured one.
	hostFaceDefaultPort = "8089"
	// hostFaceTokenEnv names the environment variable holding the request
	// credential.
	hostFaceTokenEnv = "TAGENT_RL_AUTH_TOKEN"
	// hostFaceBodyLimit is the byte ceiling of one task request body.
	hostFaceBodyLimit = 1 << 20
	// hostShutdownGrace is the span a close gives the in-flight requests.
	hostShutdownGrace = 5 * time.Second
)

// hostFace serves the host's three loopback endpoints: the liveness probe a
// supervisor polls, the task endpoint an inbound channel posts to, and the
// reload endpoint a supervisor posts to after it rewrites the configuration
// source.
type hostFace struct {
	ctx      context.Context
	host     *Host
	token    string
	listener net.Listener
	server   *http.Server
	done     chan struct{}
}

// newHostFace opens the endpoint address of one host beside the request
// credential it accepts; an empty credential leaves the loopback peer as the
// only admitted subject.
func newHostFace(ctx context.Context, host *Host, token string) (*hostFace, error) {
	address := hostFaceListenAddr(os.Getenv(hostFacePortEnv))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("tagent: listen on %s: %w", address, err)
	}
	face := &hostFace{
		ctx:      ctx,
		host:     host,
		token:    strings.TrimSpace(token),
		listener: listener,
		done:     make(chan struct{}),
	}
	face.server = &http.Server{Handler: face.Handler()}
	return face, nil
}

// metrics answers the counter face of the host in the Prometheus text format:
// the admitted turns, the committed facts and the accepted deliveries, written
// by hand so the endpoint carries no client library of its own. The endpoint
// sits in the health family: it is reachable on the loopback listener beside
// the liveness probe and asks for no credential, because it exposes counts and
// never message content.
func (f *hostFace) metrics(w http.ResponseWriter, _ *http.Request) {
	turns, facts, deliveries := f.host.counters()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprint(w, "# HELP tagent_turns_total Invocations admitted by this host.\n")
	fmt.Fprint(w, "# TYPE tagent_turns_total counter\n")
	fmt.Fprintf(w, "tagent_turns_total %d\n", turns)
	fmt.Fprint(w, "# HELP tagent_facts_total Committed facts held by this run.\n")
	fmt.Fprint(w, "# TYPE tagent_facts_total counter\n")
	fmt.Fprintf(w, "tagent_facts_total %d\n", facts)
	fmt.Fprint(w, "# HELP tagent_deliveries_total Messages accepted by the delivery face.\n")
	fmt.Fprint(w, "# TYPE tagent_deliveries_total counter\n")
	fmt.Fprintf(w, "tagent_deliveries_total %d\n", deliveries)
}

// counters reads the counter faces the metrics endpoint publishes: the
// invocations admitted by this host, the facts the run holds and the
// notifications its delivery face accepted.
func (h *Host) counters() (uint64, int, uint64) {
	h.mu.Lock()
	turns := h.invocations
	h.mu.Unlock()
	facts := 0
	if h.run != nil {
		facts = h.run.CommittedFacts()
	}
	return turns, facts, h.deliveries.acceptedCount()
}

// Handler returns the endpoints of the face: GET /healthz answers the liveness
// probe, GET /metrics answers the counters, POST /task admits one injection,
// and POST /reload rereads the configuration source and answers the model
// changes it judged.
func (f *hostFace) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	})
	mux.HandleFunc("GET /metrics", f.metrics)
	mux.HandleFunc("POST /task", f.inject)
	mux.HandleFunc("POST /reload", f.reload)
	return mux
}

// reload rereads the configuration source and answers the model changes the
// reload judged. The endpoint authenticates exactly as the task endpoint does:
// a configured credential must arrive as a bearer token, and a face beside no
// credential admits the loopback peer only.
func (f *hostFace) reload(w http.ResponseWriter, r *http.Request) {
	if err := f.authorize(r); err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="tagent"`)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	report, err := f.host.Reload(f.ctx)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrHostClosed):
			status = http.StatusServiceUnavailable
		case errors.Is(err, ErrHostReloadStructural):
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(report); err != nil {
		assemblyWarn("tagent: write the reload answer: %v", err)
	}
}

// inject admits one injection and answers before its turn runs: the body's user
// content is driven at the user session the host admitted last, in the host
// context so a shutdown still reaches the turn.
func (f *hostFace) inject(w http.ResponseWriter, r *http.Request) {
	if err := f.authorize(r); err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="tagent"`)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, hostFaceBodyLimit))
	if err != nil {
		http.Error(w, fmt.Sprintf("read the task body: %v", err), http.StatusBadRequest)
		return
	}
	var injection hostInjection
	if err := json.Unmarshal(body, &injection); err != nil {
		http.Error(w, fmt.Sprintf("decode the task body: %v", err), http.StatusBadRequest)
		return
	}
	text := injection.userText()
	if text == "" {
		http.Error(w, ErrHostEmptyTask.Error(), http.StatusBadRequest)
		return
	}
	var addr event.Address
	if label := strings.TrimSpace(injection.Session); label != "" {
		addr = f.host.injectAddress(label)
	} else {
		var ok bool
		addr, ok = f.host.userAddress()
		if !ok {
			http.Error(w, ErrHostNoActiveSession.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusAccepted)
	go func() {
		if _, err := f.host.Invoke(f.ctx, InvokeRequest{
			Address:       addr,
			TriggerSource: injection.TriggerSource,
			Input:         []model.Message{{Role: model.RoleUser, Content: text}},
		}); err != nil {
			assemblyWarn("tagent: the injected task failed (session=%s): %v", addr.SessionID, err)
		}
	}()
}

// authorize judges one task request: a configured credential must arrive as a
// bearer token and match in constant time, and a face beside no credential
// admits the loopback peer only.
func (f *hostFace) authorize(r *http.Request) error {
	if f.token != "" {
		scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			return ErrHostUnauthenticated
		}
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(f.token)) != 1 {
			return ErrHostUnauthenticated
		}
		return nil
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ErrHostUnauthenticated
	}
	if !net.ParseIP(host).IsLoopback() {
		return ErrHostUnauthenticated
	}
	return nil
}

// start serves the endpoints on the opened listener.
func (f *hostFace) start() {
	go func() {
		defer close(f.done)
		if err := f.server.Serve(f.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			assemblyWarn("tagent: the task face stopped: %v", err)
		}
	}()
}

// stop shuts the endpoints down and waits for the in-flight handlers.
func (f *hostFace) stop(ctx context.Context) error {
	if f == nil || f.server == nil {
		return nil
	}
	err := f.server.Shutdown(ctx)
	if err != nil {
		_ = f.server.Close()
	}
	<-f.done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// addr reports the address the face listens on.
func (f *hostFace) addr() string {
	if f == nil || f.listener == nil {
		return ""
	}
	return f.listener.Addr().String()
}

// hostFaceListenAddr reports the listen address of the face: the loopback
// interface on the named port, and on the default port when the value names no
// port number.
func hostFaceListenAddr(port string) string {
	if _, err := strconv.Atoi(port); err != nil {
		port = hostFaceDefaultPort
	}
	return net.JoinHostPort("127.0.0.1", port)
}
