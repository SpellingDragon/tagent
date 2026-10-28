//go:build smoke

// smoke_bench_test.go 度量全 mock 装配后的微信宿主吞吐：脚本模型零延迟，模型端点、
// 微信出站与审批面都是进程内替身，每条 benchmark 都走生产 Drive 入口。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#module-layout
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory/replay"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

const (
	// benchWaitTimeout bounds one asynchronous settle delivery.
	benchWaitTimeout = 60 * time.Second
	// benchSettlePause lets the settle driver leave its one in-flight slot.
	benchSettlePause = 200 * time.Microsecond
	// benchNoFoldTokens declares a model window no benchmarked turn fills.
	benchNoFoldTokens = 200000
	// benchFoldTokens declares a model window every fold turn exceeds.
	benchFoldTokens = 150
	// benchFoldWarmTurns is the number of warm-up turns before the fold timer starts.
	benchFoldWarmTurns = 10
	// benchSyncWarmTurns is the number of warm-up turns before the sync timer starts.
	benchSyncWarmTurns = 10
	// benchParWarmTurns is the number of warm-up turns each parallel session drives.
	benchParWarmTurns = 5
	// benchSyncMarkers starts the sync benchmark marker numbers.
	benchSyncMarkers = 100000
	// benchSettleMarkers starts the settle benchmark marker numbers.
	benchSettleMarkers = 200000
	// benchFoldMarkers starts the fold benchmark marker numbers.
	benchFoldMarkers = 300000
	// benchParMarkers starts the parallel benchmark marker numbers.
	benchParMarkers = 400000
)

// benchLog discards every log line.
type benchLog struct{}

// Debug implements log.Logger.
func (benchLog) Debug(...any) {}

// Debugf implements log.Logger.
func (benchLog) Debugf(string, ...any) {}

// Info implements log.Logger.
func (benchLog) Info(...any) {}

// Infof implements log.Logger.
func (benchLog) Infof(string, ...any) {}

// Warn implements log.Logger.
func (benchLog) Warn(...any) {}

// Warnf implements log.Logger.
func (benchLog) Warnf(string, ...any) {}

// Error implements log.Logger.
func (benchLog) Error(...any) {}

// Errorf implements log.Logger.
func (benchLog) Errorf(string, ...any) {}

// Fatal implements log.Logger without ending the process.
func (benchLog) Fatal(...any) {}

// Fatalf implements log.Logger without ending the process.
func (benchLog) Fatalf(string, ...any) {}

// benchChannel is the recording channel beside the texts each benchmark waits on.
type benchChannel struct {
	*fakeChannel

	mu     sync.Mutex
	bodies []string
	wake   chan struct{}
}

// newBenchChannel returns a recording channel whose waiters wake on every text.
func newBenchChannel() *benchChannel {
	return &benchChannel{fakeChannel: newFakeChannel(), wake: make(chan struct{}, 1)}
}

// SendTextToUser records one text delivery and wakes the waiters.
func (c *benchChannel) SendTextToUser(ctx context.Context, to, text string) error {
	err := c.fakeChannel.SendTextToUser(ctx, to, text)
	c.mu.Lock()
	c.bodies = append(c.bodies, text)
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return err
}

// textCount reports how many texts the channel delivered.
func (c *benchChannel) textCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bodies)
}

// lastText reports the delivery count and the body of the last text.
func (c *benchChannel) lastText() (int, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) == 0 {
		return 0, ""
	}
	return len(c.bodies), c.bodies[len(c.bodies)-1]
}

// waitTexts waits until want texts were delivered and reports those after before.
func (c *benchChannel) waitTexts(b *testing.B, before, want int) []string {
	b.Helper()
	timeout := time.NewTimer(benchWaitTimeout)
	defer timeout.Stop()
	for {
		c.mu.Lock()
		got := len(c.bodies)
		if got >= want {
			out := append([]string(nil), c.bodies[before:want]...)
			c.mu.Unlock()
			return out
		}
		c.mu.Unlock()
		select {
		case <-c.wake:
		case <-timeout.C:
			b.Fatalf("waited %s for %d text deliveries, got %d", benchWaitTimeout, want, got)
		}
	}
}

// benchRig is one assembled host over the recording channel.
type benchRig struct {
	b        *testing.B
	app      *wechatApp
	host     *assembly.Host
	channel  *benchChannel
	provider *smokeProvider
	cfg      *smokeConfig
}

// benchMarker spells one unique user-turn marker for the scripted model.
func benchMarker(base, i int) string {
	return fmt.Sprintf("SMOKE-R%d", base+i)
}

// benchConfig renders a smoke config whose entry agent declares the given window.
func benchConfig(b *testing.B, maxTokens int) *smokeConfig {
	b.Helper()
	root := b.TempDir()
	cfg := &smokeConfig{
		Root:         root,
		WorkspaceDir: filepath.Join(root, "workspace"),
		ConfigDir:    filepath.Join(root, "wechat-config"),
		ContextDir:   filepath.Join(root, "wechat-context"),
		ApprovalDir:  filepath.Join(root, "approvals"),
		PromptDir:    filepath.Join(root, "prompts"),
		StorageRoot:  filepath.Join(root, "storage"),
		ConfigPath:   filepath.Join(root, "tagent.yaml"),
		Endpoint:     "http://127.0.0.1:1/v1",
		Model:        smokeEntryModel,
	}
	for _, dir := range []string{
		cfg.WorkspaceDir, cfg.ConfigDir, cfg.ContextDir,
		cfg.ApprovalDir, cfg.PromptDir, cfg.StorageRoot,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatalf("create the bench directory %s: %v", dir, err)
		}
	}
	prompt := filepath.Join(cfg.PromptDir, "smoke-meditation.md")
	if err := os.WriteFile(prompt, []byte("Reflect over the observed smoke session and deliver one summary.\n"), 0o644); err != nil {
		b.Fatalf("write the meditation prompt: %v", err)
	}
	cfg.Doc = func(c *smokeConfig) string {
		base := *c
		base.Doc = nil
		raw := strings.Replace(base.document(), "max_tokens: 2000",
			fmt.Sprintf("max_tokens: %d", maxTokens), 1)
		return strings.Replace(raw, "interval: 5s", "interval: 24h", 1)
	}
	writeBenchConfig(b, cfg)
	return cfg
}

// writeBenchConfig writes the rendered host document.
func writeBenchConfig(b *testing.B, cfg *smokeConfig) {
	b.Helper()
	if err := os.WriteFile(cfg.ConfigPath, []byte(cfg.document()), 0o644); err != nil {
		b.Fatalf("write the host document: %v", err)
	}
}

// newBenchProvider starts the scripted endpoint and closes it with the benchmark.
func newBenchProvider(b *testing.B, file string) *smokeProvider {
	b.Helper()
	provider := &smokeProvider{file: file}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	b.Cleanup(provider.server.Close)
	return provider
}

// bootBenchRig assembles the production wechat host over the recording channel.
func bootBenchRig(b *testing.B, maxTokens int) *benchRig {
	b.Helper()
	b.Setenv("SMOKE_API_KEY", "smoke-key")
	b.Setenv("TAGENT_HTTP_PORT", "0")
	b.Setenv("TAGENT_RL_AUTH_TOKEN", "")
	b.Setenv("TAGENT_SESSION_ID", "smoke-bench")
	b.Setenv("TAGENT_LOG_LEVEL", "info")
	previousDefault, previousContext := log.Default, log.ContextDefault
	log.Default, log.ContextDefault = benchLog{}, benchLog{}
	b.Cleanup(func() {
		log.Default, log.ContextDefault = previousDefault, previousContext
	})

	cfg := benchConfig(b, maxTokens)
	provider := newBenchProvider(b, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	writeBenchConfig(b, cfg)
	appCfg, err := loadWechatConfig(cfg.ConfigPath)
	if err != nil {
		b.Fatalf("load the wechat section: %v", err)
	}
	gateway := newApprovalGateway(appCfg.Governance, appCfg.App.Wechat.Approvers)
	app, err := newWechatApp(appCfg, gateway)
	if err != nil {
		b.Fatalf("assemble the wechat application: %v", err)
	}
	channel := newBenchChannel()
	options := append(app.options(), assembly.WithApprovals(gateway.Manager()))
	host, err := assembly.Boot(cfg.ConfigPath, options...)
	if err != nil {
		b.Fatalf("boot the host: %v", err)
	}
	app.host.invoke = func(ctx context.Context, req assembly.InvokeRequest) (bool, error) {
		turn, err := host.Invoke(ctx, req)
		if err != nil {
			return false, err
		}
		return turn.Terminal == tagentmodel.KindFailed, nil
	}
	app.face = host.FaceAddr()
	gateway.SetSender(channel)
	app.typing = bindChannel(app.host, channel, appCfg.App.Wechat)
	registerHandlers(app.host, channel, gateway, appCfg.App.Wechat, app.typing)
	if app.face == "" {
		b.Fatal("the host face listens on no address")
	}
	b.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := host.Close(ctx); err != nil {
			b.Errorf("close the bench host: %v", err)
		}
	})
	return &benchRig{b: b, app: app, host: host, channel: channel, provider: provider, cfg: cfg}
}

// drive drives one user turn through the production inbound path.
func (r *benchRig) drive(chat, text string) error {
	_, err := r.app.host.Drive(context.Background(), inboundMessage{
		ChatID: chat, UserName: chat, Text: text,
	})
	return err
}

// mustDrive drives one user turn and fails the benchmark on error.
func (r *benchRig) mustDrive(chat, text string) {
	r.b.Helper()
	if err := r.drive(chat, text); err != nil {
		r.b.Fatalf("drive the session %s: %v", chat, err)
	}
}

// mustSettle drives one delegated turn and waits for its ack and settle delivery.
func (r *benchRig) mustSettle(chat, marker string) {
	r.b.Helper()
	before := r.channel.textCount()
	r.mustDrive(chat, marker+" smoke delegate artifact")
	bodies := r.channel.waitTexts(r.b, before, before+2)
	time.Sleep(benchSettlePause)
	ack, settled := false, false
	for _, body := range bodies {
		if body == "delegate ack "+marker {
			ack = true
		}
		if body == "settle-ack "+marker {
			settled = true
		}
	}
	if !ack || !settled {
		r.b.Fatalf("settle turn %s delivered %q, want the delegate ack and the settle ack", marker, bodies)
	}
}

// addressOf reports the address one chat is bound to.
func (r *benchRig) addressOf(chat string) event.Address {
	r.b.Helper()
	addr, err := r.app.host.router.AddressFor(chat)
	if err != nil {
		r.b.Fatalf("address of %s: %v", chat, err)
	}
	return addr
}

// compressFacts reports the compression judgments filed on one session chain.
func (r *benchRig) compressFacts(chat string) int {
	r.b.Helper()
	session := r.addressOf(chat).SessionID
	filed := 0
	for _, fact := range r.host.Run().SessionFacts(event.Address{SessionID: session}) {
		if fact.Type == event.CompressRequestType {
			filed++
		}
	}
	return filed
}

// foldProducts reports the fold products the projection of one session carries.
func (r *benchRig) foldProducts(chat string) int {
	r.b.Helper()
	entries, carried := r.host.Run().ReplayEntries(r.addressOf(chat).SessionID)
	if !carried {
		r.b.Fatalf("session %s carries no projection", chat)
	}
	products := 0
	for _, entry := range entries {
		if entry.Type == replay.ProjectionTypeHistorySummary || entry.Type == replay.ProjectionTypeSegmentFold {
			products++
		}
	}
	return products
}

// BenchmarkSyncRound measures one synchronous turn: drive, model answer and delivery.
func BenchmarkSyncRound(b *testing.B) {
	rig := bootBenchRig(b, benchNoFoldTokens)
	chat := "bench-sync"
	for i := 0; i < benchSyncWarmTurns; i++ {
		rig.mustDrive(chat, benchMarker(benchSyncMarkers, i)+" plain question")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		marker := benchMarker(benchSyncMarkers, benchSyncWarmTurns+i)
		rig.mustDrive(chat, marker+" plain question")
		count, body := rig.channel.lastText()
		if count != benchSyncWarmTurns+i+1 || body != "answer "+marker {
			b.Fatalf("turn %d delivered %q at count %d, want %q", i, body, count, "answer "+marker)
		}
	}
}

// BenchmarkSettleTurnaround measures one delegated round up to its settle delivery.
func BenchmarkSettleTurnaround(b *testing.B) {
	rig := bootBenchRig(b, benchNoFoldTokens)
	chat := "bench-settle"
	rig.mustSettle(chat, benchMarker(benchSettleMarkers, 0))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rig.mustSettle(chat, benchMarker(benchSettleMarkers, i+1))
	}
}

// BenchmarkFoldRound measures a folding window beside a window no turn fills.
func BenchmarkFoldRound(b *testing.B) {
	cases := []struct {
		name      string
		maxTokens int
		fold      bool
	}{
		{"fold=off", benchNoFoldTokens, false},
		{"fold=on", benchFoldTokens, true},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			rig := bootBenchRig(b, tc.maxTokens)
			chat := "bench-fold"
			for i := 0; i < benchFoldWarmTurns; i++ {
				rig.mustDrive(chat, benchMarker(benchFoldMarkers, i)+" plain question")
			}
			base := rig.channel.textCount()
			filedBefore := rig.compressFacts(chat)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rig.mustDrive(chat, benchMarker(benchFoldMarkers, benchFoldWarmTurns+i)+" plain question")
			}
			b.StopTimer()
			if got, want := rig.channel.textCount(), base+b.N; got != want {
				b.Fatalf("the fold window delivered %d texts over %d turns, want %d", got, b.N, want)
			}
			filed := rig.compressFacts(chat) - filedBefore
			products := rig.foldProducts(chat)
			if tc.fold && (filed == 0 || products == 0) {
				b.Fatalf("the small window filed %d compression judgments and carries %d products", filed, products)
			}
			if !tc.fold && (filed != 0 || products != 0) {
				b.Fatalf("the roomy window filed %d compression judgments and carries %d products", filed, products)
			}
			b.ReportMetric(float64(filed)/float64(b.N), "folds/op")
			b.ReportMetric(float64(products), "products")
		})
	}
}

// BenchmarkConcurrentSessions measures parallel drives over distinct sessions.
func BenchmarkConcurrentSessions(b *testing.B) {
	for _, threads := range []int{3, 10} {
		b.Run(fmt.Sprintf("threads-%d", threads), func(b *testing.B) {
			rig := bootBenchRig(b, benchNoFoldTokens)
			free := make(chan string, threads)
			for i := 0; i < threads; i++ {
				chat := fmt.Sprintf("bench-par-%d", i)
				for j := 0; j < benchParWarmTurns; j++ {
					rig.mustDrive(chat, benchMarker(benchParMarkers, j*threads+i)+" plain question")
				}
				free <- chat
			}
			base := rig.channel.textCount()
			var sequence atomic.Int64
			var mu sync.Mutex
			var firstErr error
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					chat := <-free
					marker := benchMarker(benchParMarkers, threads*benchParWarmTurns+int(sequence.Add(1)))
					err := rig.drive(chat, marker+" plain question")
					free <- chat
					if err != nil {
						mu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						mu.Unlock()
					}
				}
			})
			b.StopTimer()
			mu.Lock()
			err := firstErr
			mu.Unlock()
			if err != nil {
				b.Fatalf("a parallel drive failed: %v", err)
			}
			if got, want := rig.channel.textCount(), base+int(sequence.Load()); got != want {
				b.Fatalf("parallel deliveries = %d, want %d", got, want)
			}
		})
	}
}
