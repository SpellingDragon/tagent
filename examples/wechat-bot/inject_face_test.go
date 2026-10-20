// inject_face_test 钉住 宿主 HTTP 面的判据：探活无凭据即答、注入按凭据或回环受判、
// 注入在最近活跃会话上开一轮而响应先行返回、无会话与空正文分别具名拒绝。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// loopbackPeer is the remote address a same-host injection arrives from.
const loopbackPeer = "127.0.0.1:52311"

// foreignPeer is the remote address an injection from another host arrives from.
const foreignPeer = "203.0.113.9:41234"

// recordingDrive is the stand-in host the face drives: it reports the chat it was
// told and records every delivery admitted through it.
type recordingDrive struct {
	chat string

	mu       sync.Mutex
	admitted []inboundMessage
	driven   chan inboundMessage
}

// newRecordingDrive returns the stand-in host beside the chat it reports.
func newRecordingDrive(chat string) *recordingDrive {
	return &recordingDrive{chat: chat, driven: make(chan inboundMessage, 4)}
}

// mostRecentChat reports the chat the stand-in host was told to.
func (d *recordingDrive) mostRecentChat() string { return d.chat }

// Drive records the admitted delivery and reports the session its chat spells.
func (d *recordingDrive) Drive(_ context.Context, in inboundMessage) (event.Address, error) {
	d.mu.Lock()
	d.admitted = append(d.admitted, in)
	d.mu.Unlock()
	d.driven <- in
	return event.Address{AgentID: "tagent", SessionID: event.SessionID("test:" + in.ChatID)}, nil
}

// count reports how many deliveries the stand-in host admitted.
func (d *recordingDrive) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.admitted)
}

// postInjection serves one injection request through the face and reports the
// recorded status.
func postInjection(t *testing.T, face *taskFace, peer, token, body string) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/task", strings.NewReader(body))
	request.RemoteAddr = peer
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	face.Handler().ServeHTTP(recorder, request)
	return recorder.Code
}

// TestTaskFaceServesLivenessProbe pins the probe: the endpoint answers 200 with a body beside no credential.
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestTaskFaceServesLivenessProbe(t *testing.T) {
	face := newTaskFace(context.Background(), newRecordingDrive("chat-7"), "")
	recorder := httptest.NewRecorder()

	face.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusOK || recorder.Body.String() != "ok" {
		t.Fatalf("probe = %d %q, want 200 ok", recorder.Code, recorder.Body.String())
	}
}

// TestTaskFaceAdmitsInjectionAtTheMostRecentChat pins the admitted path: 202 first, then the chat that last delivered.
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestTaskFaceAdmitsInjectionAtTheMostRecentChat(t *testing.T) {
	drive := newRecordingDrive("chat-7")
	face := newTaskFace(context.Background(), drive, "")

	code := postInjection(t, face, loopbackPeer, "", `{"messages":[{"role":"user","content":"邮件正文"}],"trigger_source":"user"}`)
	if code != http.StatusAccepted {
		t.Fatalf("injection status = %d, want 202", code)
	}
	select {
	case got := <-drive.driven:
		if got.ChatID != "chat-7" || got.Text != "邮件正文" {
			t.Fatalf("driven = %+v, want the most recent chat beside the injected content", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the injected turn never reached the host")
	}
}

// TestTaskFaceRefusesForeignPeerWithoutCredential pins the closed default: beside no credential only the loopback peer is admitted.
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestTaskFaceRefusesForeignPeerWithoutCredential(t *testing.T) {
	drive := newRecordingDrive("chat-7")
	face := newTaskFace(context.Background(), drive, "")

	code := postInjection(t, face, foreignPeer, "", `{"messages":[{"role":"user","content":"hi"}]}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("foreign injection status = %d, want 401", code)
	}
	if drive.count() != 0 {
		t.Fatalf("a refused injection still reached the host: %d", drive.count())
	}
}

// TestTaskFaceRequiresConfiguredCredential pins the credential gate: a configured token must arrive as a matching bearer token.
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestTaskFaceRequiresConfiguredCredential(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"hi"}]}`
	face := newTaskFace(context.Background(), newRecordingDrive("chat-7"), "s3cret")

	cases := []struct {
		name  string
		peer  string
		token string
		want  int
	}{
		{name: "missing token", peer: loopbackPeer, want: http.StatusUnauthorized},
		{name: "wrong token", peer: loopbackPeer, token: "guess", want: http.StatusUnauthorized},
		{name: "matching token", peer: foreignPeer, token: "s3cret", want: http.StatusAccepted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := postInjection(t, face, c.peer, c.token, body); got != c.want {
				t.Fatalf("status = %d, want %d", got, c.want)
			}
		})
	}
}

// TestTaskFaceRefusesInjectionWithoutUserContent pins the body gate: no user message, no turn.
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestTaskFaceRefusesInjectionWithoutUserContent(t *testing.T) {
	drive := newRecordingDrive("chat-7")
	face := newTaskFace(context.Background(), drive, "")

	code := postInjection(t, face, loopbackPeer, "", `{"messages":[{"role":"system","content":"旁白"}]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("contentless injection status = %d, want 400", code)
	}
	if drive.count() != 0 {
		t.Fatalf("a contentless injection still reached the host: %d", drive.count())
	}
}

// TestTaskFaceRefusesInjectionWithoutActiveChat pins the missing-target refusal: 503, never an invented session.
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestTaskFaceRefusesInjectionWithoutActiveChat(t *testing.T) {
	drive := newRecordingDrive("")
	face := newTaskFace(context.Background(), drive, "")

	code := postInjection(t, face, loopbackPeer, "", `{"messages":[{"role":"user","content":"hi"}]}`)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("target-less injection status = %d, want 503", code)
	}
	if drive.count() != 0 {
		t.Fatalf("a target-less injection still reached the host: %d", drive.count())
	}
}

// TestInjectionUserTextPicksTheLastUserMessage pins the body reading: the last non-empty user message is the one a turn reads.
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestInjectionUserTextPicksTheLastUserMessage(t *testing.T) {
	injection := taskInjection{Messages: []injectionMessage{
		{Role: "user", Content: "第一句"},
		{Role: "assistant", Content: "旁白"},
		{Role: "user", Content: "  "},
		{Role: "user", Content: "最后一句"},
	}}
	if got := injection.userText(); got != "最后一句" {
		t.Fatalf("user text = %q, want the last non-empty user message", got)
	}
}

// TestFaceAddrBindsLoopbackOnTheNamedPort pins the listen address: loopback on the named port, and the default beside no port.
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
func TestFaceAddrBindsLoopbackOnTheNamedPort(t *testing.T) {
	cases := map[string]string{
		"18089": "127.0.0.1:18089",
		"":      "127.0.0.1:" + faceDefaultPort,
		"http":  "127.0.0.1:" + faceDefaultPort,
	}
	for in, want := range cases {
		if got := faceAddr(in); got != want {
			t.Fatalf("faceAddr(%q) = %q, want %q", in, got, want)
		}
	}
}
