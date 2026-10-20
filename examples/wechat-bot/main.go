// main.go 只做 wiring：读配置 → 装配 → 登录 → 注册挂点 → 启动 → 注入面。
// 微信协议语义（收发/文件/路由/去重/send/审批/通知）全部在 wechat.go；
// 声明、模型与工具面的构造走 api/document、api/config 与 tagent.Assemble 现有入口。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

func main() {
	configPath := "tagent.yaml"
	if envPath := os.Getenv("TAGENT_CONFIG"); envPath != "" {
		configPath = envPath
	}

	doc, err := loadHostDocument(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load config failed: %v\n", err)
		os.Exit(1)
	}

	wechatCfg := doc.App.Wechat
	applyWechatDefaults(&wechatCfg, os.Getenv("TAGENT_WECHAT_WORKSPACE_DIR"))

	gateway := newApprovalGateway(doc.Governance, wechatCfg.Approvers)
	if handled, code := runApprovalCLI(gateway, os.Args[1:]); handled {
		os.Exit(code)
	}

	if err := wechatCfg.EnsureDirs(); err != nil {
		fmt.Fprintf(os.Stderr, "Create dirs failed: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	host, err := assembleHost(ctx, doc, gateway, NewSeenStore(wechatCfg.ConfigDir, slog.Default()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Assemble runtime failed: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := host.Close(shutdownCtx); err != nil {
			log.Warnf("[host] close: %v", err)
		}
	}()

	fmt.Println("===========================================")
	fmt.Println("  tagent WeChat Bot")
	fmt.Println("===========================================")
	fmt.Printf("  Agent Name:  %s\n", doc.Entry)
	fmt.Printf("  Model:       %s\n", doc.modelName(doc.Entry))
	fmt.Printf("  Provider:    %s\n", doc.Provider)
	fmt.Printf("  Config:      %s\n", configPath)
	fmt.Println("===========================================")

	go maybeInjectReincarnationNotice(host.inbox, nil, doc.Entry, "run", noticeWaitMax)
	go maybeConsumeSystemAlert(host.inbox, "run", 5*time.Second)

	bot := newWechatBot(wechatCfg)
	gateway.SetSender(bot)
	if err := login(ctx, bot); err != nil {
		fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
		os.Exit(1)
	}

	typingActive := bindChannel(host, bot, wechatCfg)
	if err := host.startMeditation(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Start the meditation driver failed: %v\n", err)
		os.Exit(1)
	}
	registerHandlers(host, bot, gateway, wechatCfg, typingActive)
	serveFace(ctx, host)

	fmt.Println("Bot is running. Press Ctrl+C to stop.")
	if err := bot.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "Bot stopped with error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Bot stopped gracefully.")
}
