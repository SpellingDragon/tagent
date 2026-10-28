// main.go 只做 wiring：读应用配置 → Boot 装配 → 绑定微信通道 → 运行。
// 微信协议语义（收发/文件/路由/去重/审批/通知）在 wechat*.go；
// 声明、模型、工具面、storage、meditation 与注入面全部由 assembly.Boot 装配。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#startup-and-routing
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/SpellingDragon/tagent/assembly"
)

func main() {
	path := configPath()
	cfg, err := loadWechatConfig(path)
	dieIf(err, "Load config")

	gateway := newApprovalGateway(cfg.Governance, cfg.App.Wechat.Approvers)
	if handled, code := runApprovalCLI(gateway, os.Args[1:]); handled {
		os.Exit(code)
	}

	app, err := newWechatApp(cfg, gateway)
	dieIf(err, "Prepare the wechat channel")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	host, err := assembly.Boot(path, app.options()...)
	dieIf(err, "Boot the host")
	defer app.close(host)

	if err := app.bindWechat(ctx, host); err != nil {
		app.close(host)
		dieIf(err, "Bind the wechat channel")
	}
	app.banner(path)
	if err := app.run(ctx); err != nil {
		app.close(host)
		dieIf(err, "Run the wechat channel")
	}
}
