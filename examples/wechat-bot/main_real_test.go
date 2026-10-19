package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/SpellingDragon/wechat-robot-go/wechat"
)

// TestRealWechat is the resource gate of the real WeChat send face.
// - The authorized branch delivers one real message beside credentials and explicit send authorization.
// - A half-provisioned gate is refused by name.
// - No gate skips as unverified so the face is never claimed by a stand-in.
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#real-send-gate
func TestRealWechat(t *testing.T) {
	token := os.Getenv("TAGENT_WECHAT_TOKEN")
	target := os.Getenv("TAGENT_WECHAT_REAL_TARGET")
	authorized := os.Getenv("TAGENT_WECHAT_SEND_AUTHORIZED")
	switch {
	case token == "" && target == "" && authorized == "":
		t.Skip("real WeChat face unverified: TAGENT_WECHAT_TOKEN, TAGENT_WECHAT_REAL_TARGET and TAGENT_WECHAT_SEND_AUTHORIZED are unset (resource gate)")
	case token == "" || target == "" || authorized == "":
		t.Fatalf("the WeChat resource gate is half provisioned: every one of TAGENT_WECHAT_TOKEN, TAGENT_WECHAT_REAL_TARGET and TAGENT_WECHAT_SEND_AUTHORIZED is required beside none")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bot := wechat.NewBot(wechat.WithTokenFile(token))
	if err := bot.Login(ctx, func(string) {}); err != nil {
		t.Fatalf("the real WeChat login refused the token file %q: %v", token, err)
	}
	content := "tagent real WeChat send face: authorized delivery verification"
	if err := bot.SendTextToUser(ctx, target, content); err != nil {
		t.Fatalf("the real send to %q failed: %v", target, err)
	}
	t.Logf("real send delivered to %q (%d bytes of text)", target, len(content))
}
