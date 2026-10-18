// main_test 钉住 宿主发送通路的会址纪律：目标只按精确会址解析，未绑定的会址具名拒绝并留回执，
// 且测试永不构造真实微信通道。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#outbound-delivery
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/event"
)

// newTestHostSender returns one outbound path over the stand-in channel, routed
// through the given router and receipts.
func newTestHostSender(bot *mockOutbound, router *SessionRouter) *hostSender {
	return &hostSender{
		bot:          bot,
		longText:     nil,
		typingActive: &sync.Map{},
		router:       router,
		inj:          &recordingInjector{},
		workspaceDir: "",
	}
}

// TestHostSenderDeliversAtTheBoundAddress 钉住 送达目标取自会址自己的绑定，通道收到的正文即请求正文。
func TestHostSenderDeliversAtTheBoundAddress(t *testing.T) {
	bot := &mockOutbound{}
	router, err := NewSessionRouter("tagent", "test:")
	if err != nil {
		t.Fatal(err)
	}
	addr, err := router.AddressFor("chat-7")
	if err != nil {
		t.Fatal(err)
	}
	if err := router.Bind(addr, "chat-7"); err != nil {
		t.Fatal(err)
	}
	sender := newTestHostSender(bot, router)

	label, err := sender.sendToUser(context.Background(), sendRequest{
		addr: addr, userName: "alice", triggerSource: "user", content: "hello",
	})
	if err != nil {
		t.Fatalf("sendToUser: %v", err)
	}
	if label != "alice(chat-7)" {
		t.Fatalf("delivery label = %q, want the bound chat beside its user", label)
	}
	if got := bot.texts(); len(got) != 1 || got[0].Target != "chat-7" || got[0].Body != "hello" {
		t.Fatalf("text deliveries = %v, want one delivery to chat-7", got)
	}
}

// TestHostSenderFailureReceiptsTheTerminal 钉住 通道失败仍记 send-failed 回执，且失败原因随 error 回调用方。
func TestHostSenderFailureReceiptsTheTerminal(t *testing.T) {
	bot := &mockOutbound{failText: errors.New("wechat: session expired")}
	router, err := NewSessionRouter("tagent", "test:")
	if err != nil {
		t.Fatal(err)
	}
	addr, err := router.AddressFor("chat-7")
	if err != nil {
		t.Fatal(err)
	}
	if err := router.Bind(addr, "chat-7"); err != nil {
		t.Fatal(err)
	}
	records := &recordingInjector{}
	sender := &hostSender{bot: bot, typingActive: &sync.Map{}, router: router, inj: records}

	if _, err := sender.sendToUser(context.Background(), sendRequest{
		addr: addr, triggerSource: "send-tool", content: "发不出去的一句话",
	}); err == nil || !strings.Contains(err.Error(), "session expired") {
		t.Fatalf("channel failure = %v, want the named reason", err)
	}
	if got := bot.texts(); len(got) != 0 {
		t.Fatalf("a failed send counted as a delivery: %v", got)
	}
	if len(records.bodies) != 1 || !strings.Contains(records.bodies[0], "send-failed") {
		t.Fatalf("receipts = %v, want one send-failed terminal", records.bodies)
	}
}

// TestHostSenderRefusesUnboundAddressWithoutFallback 钉住 未绑定的会址即使有其它已绑定会话也拒绝送达，并留一条 no-target 回执。
func TestHostSenderRefusesUnboundAddressWithoutFallback(t *testing.T) {
	bot := &mockOutbound{}
	router, err := NewSessionRouter("tagent", "test:")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := router.AddressFor("chat-owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := router.Bind(owner, "chat-owner"); err != nil {
		t.Fatal(err)
	}
	sender := newTestHostSender(bot, router)
	stray := event.Address{AgentID: "tagent", SessionID: "test:never-bound"}

	if _, err := sender.sendToUser(context.Background(), sendRequest{
		addr: stray, triggerSource: "user", content: "nobody asked for this",
	}); !errors.Is(err, errNoSendTarget) {
		t.Fatalf("unbound delivery = %v, want errNoSendTarget", err)
	}
	if got := bot.recorded(); len(got) != 0 {
		t.Fatalf("an unbound address still reached the channel: %v", got)
	}
	receipts := sender.inj.(*recordingInjector)
	if len(receipts.addrs) != 1 || receipts.addrs[0] != stray {
		t.Fatalf("receipt addresses = %v, want the refused address", receipts.addrs)
	}
	if !strings.Contains(receipts.bodies[0], "no-target") {
		t.Fatalf("receipt = %q, want the no-target terminal", receipts.bodies[0])
	}
}

// TestGuard_TestsNeverConstructRealChannel turns the no-live-channel rule into an executable check.
// - No test file in this module may construct the live bot client or reach the network.
// - The forbidden patterns are assembled by concatenation so this file cannot match itself.
func TestGuard_TestsNeverConstructRealChannel(t *testing.T) {
	forbidden := []string{
		"wechat." + "NewBot(",
		"wechat." + "SendLongText(",
		"http." + "Get(",
		"net/http" + ".",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, pat := range forbidden {
			if strings.Contains(string(src), pat) {
				t.Errorf("%s constructs the real channel (%s) — tests never connect a live channel", e.Name(), pat)
			}
		}
	}
}
