//go:build ignore

// verify_large_file.go — SDK 大文件真链路的一次性人工验收：流式与全量两条下载路径对比
// 耗时与内存峰值、字节数与 md5 校验、反向回传验证出站。
//
// - 复用 .weixin-token.json 登录态；本脚本经 go:build ignore 排除在常规构建与 CI 之外。
// 契约: docs/wiki/examples/wechat-bot-runtime.md#large-file-acceptance
package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/SpellingDragon/wechat-robot-go/wechat"
)

func memSnapshot() runtime.MemStats {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	bot := wechat.NewBot(wechat.WithLogger(logger))

	fmt.Println("== SDK 大文件真链路验收（流式 vs 全量） ==")
	fmt.Println("登录中（复用 .weixin-token.json）...")
	if err := bot.Login(ctx, func(qr string) {
		fmt.Println("请扫码登录:")
		fmt.Println(qr)
	}); err != nil {
		fmt.Fprintf(os.Stderr, "登录失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("登录成功。请用微信向 bot 发送一个大文件（建议 ~40MB），Ctrl+C 退出。")

	outDir := filepath.Join(os.TempDir(), "wechat-large-file-verify")
	_ = os.MkdirAll(outDir, 0755)
	cdn := bot.CDNBaseURL()

	bot.OnMessage(func(ctx context.Context, msg *wechat.Message) error {
		file := msg.GetFileItem()
		if file == nil {
			return nil
		}
		fmt.Printf("\n收到文件: %s (声明大小 %s 字节, DeclaredSize=%d, 声明 md5 %s)\n",
			file.FileName, file.Length, file.DeclaredSize(), file.MD5)

		path := filepath.Join(outDir, filepath.Base(file.FileName))
		f, err := os.Create(path)
		if err != nil {
			fmt.Printf("创建落盘文件失败: %v\n", err)
			return nil
		}
		h := md5.New()

		m0 := memSnapshot()
		start := time.Now()
		written, err := bot.DownloadFileFromItemTo(ctx, file, cdn, io.MultiWriter(f, h),
			wechat.DownloadOptions{MaxSize: 100 * 1024 * 1024})
		streamElapsed := time.Since(start)
		m1 := memSnapshot()
		_ = f.Close()

		if err != nil {
			fmt.Printf("流式下载失败 (耗时 %v): %v\n", streamElapsed, err)
			_ = os.Remove(path)
			return nil
		}

		gotMD5 := hex.EncodeToString(h.Sum(nil))
		md5OK := strings.EqualFold(gotMD5, file.MD5) || file.MD5 == ""

		fmt.Println("---- 流式下载（新 API）----")
		fmt.Printf("  实际字节数: %d (声明 %s)\n", written, file.Length)
		fmt.Printf("  md5: %s (元数据 %s, 一致=%v)\n", gotMD5, file.MD5, md5OK)
		fmt.Printf("  耗时: %v (%.2f MB/s)\n", streamElapsed, float64(written)/1024/1024/streamElapsed.Seconds())
		fmt.Printf("  内存 TotalAlloc 增量: %.1f MB, HeapInuse: %.1f MB\n",
			float64(m1.TotalAlloc-m0.TotalAlloc)/1024/1024, float64(m1.HeapInuse)/1024/1024)
		fmt.Printf("  落盘: %s\n", path)

		m2 := memSnapshot()
		start = time.Now()
		data, err := bot.DownloadFileFromItem(ctx, file, cdn)
		fullElapsed := time.Since(start)
		m3 := memSnapshot()

		if err != nil {
			fmt.Printf("全量下载失败 (耗时 %v): %v\n", fullElapsed, err)
		} else {
			fmt.Println("---- 全量下载（旧 API，对照）----")
			fmt.Printf("  实际字节数: %d\n", len(data))
			fmt.Printf("  耗时: %v (%.2f MB/s)\n", fullElapsed, float64(len(data))/1024/1024/fullElapsed.Seconds())
			fmt.Printf("  内存 TotalAlloc 增量: %.1f MB, HeapInuse: %.1f MB\n",
				float64(m3.TotalAlloc-m2.TotalAlloc)/1024/1024, float64(m3.HeapInuse)/1024/1024)
			data = nil
		}

		fmt.Println("回传验证出站链路 (SendFileFromPath)...")
		start = time.Now()
		if err := bot.SendFileFromPath(ctx, msg.FromUserID, path); err != nil {
			fmt.Printf("  回传失败 (耗时 %v): %v\n", time.Since(start), err)
		} else {
			fmt.Printf("  回传成功, 耗时 %v\n", time.Since(start))
		}
		fmt.Println("验收完成，可 Ctrl+C 退出，或继续发送其他文件。")
		return nil
	})

	if err := bot.Run(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "运行出错: %v\n", err)
		os.Exit(1)
	}
}
