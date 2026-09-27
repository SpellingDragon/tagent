package agent

import (
	"context"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// §5.3 分相基准：读取之外的每一相位各自计量，且**互不混计**——
//
//	构建  = NewExecutorCandidate（不安装、不退役）
//	放弃  = 对已构造候选的 Discard（清理出口自身的代价）
//	提交  = ActivateExecutor（候选在计时区外备好，见 StopTimer/StartTimer）
//	获取  = BeginTurnLease / lease.Release（两者分别计量，不再合成一个数）
//	回收  = 退役代的 disposer 成本（观测放在计时区外，不与轮询混计）
//
// 生产形状（多 agent 组织）的对应基准在根包 org_hotreload_bench_test.go；端到端
// reload 也在那里，且**不由本文件的相位数字相减推得**（§5.3：不同批次均值不可减）。
//
// 每个基准都：①给自己构造出的每个候选留清理出口并在计时结束后清完；②关闭自己的
// ContextManager（§5.3「测试自身 CM/session/维护组件也关闭」）；③不以
// PendingRetirees==0 推断「没有泄漏」——它只被当作「本相位没漏下引用」的边界检查，
// 真正的对象归属由上面各相位的配对释放断言把关。

func benchPublishCM(b *testing.B, name string) *ContextManager {
	b.Helper()
	cm := newTestContextManager(name, &requestCapturingModel{resp: gateOKResp()},
		nil, make(chan *event.Event, 4096), nil)
	b.Cleanup(func() { _ = cm.Close() }) // 自己的组件自己关
	return cm
}

// BenchmarkNewExecutorCandidate 度量纯构建：造一个候选，不安装、不退役、不记日志。
// 上一版把已构造候选直接丢弃在循环里（b.N 个 runner 无人清理），本轮补上出口：
// 每轮先放弃上一轮的候选再构造下一轮，放弃的代价单独在 BenchmarkCandidateAbandon
// 里计量，因此这里含一次放弃（有界、每轮一次）——不假装它是零，也不把 b.N 个
// 活对象留给进程结束。
func BenchmarkNewExecutorCandidate(b *testing.B) {
	cm := benchPublishCM(b, "bench-cand")
	face := cm.ExecutorConfig()

	var prev *StagedGeneration
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if prev != nil {
			prev.Discard()
		}
		prev = cm.StageExecutor(cm.NewExecutorCandidate(face), face, nil)
	}
	b.StopTimer()
	if prev != nil {
		prev.Discard()
	}
	// 纯构建不得改变在线执行器，也不得留下未回收的退役代。
	if got := cm.ExecutorRefs(); got.PendingRetirees != 0 || got.InFlightTurns != 0 {
		b.Fatalf("construction+abandon alone must leave no retirement debt or refs: %+v", got)
	}
}

// BenchmarkCandidateAbandon 计量「放弃一个已构造候选」本身：这是热更失败路径的
// 成本，过去从未被单独看过，于是它被埋在构建或回收的数字里。
func BenchmarkCandidateAbandon(b *testing.B) {
	cm := benchPublishCM(b, "bench-abandon")
	face := cm.ExecutorConfig()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer() // staging is the OTHER phase; it must not be billed here
		st := cm.StageExecutor(cm.NewExecutorCandidate(face), face, nil)
		b.StartTimer()
		st.Discard()
	}
	b.StopTimer()
}

// BenchmarkCommitPrepared 度量「提交」——把已准备好的候选换入并退役上一代。
// 候选在**计时区外**构造（StopTimer/StartTimer），所以这个数字不再被构建成本主导；
// 回收观测也在计时区外做，不与本相位混计。
func BenchmarkCommitPrepared(b *testing.B) {
	cm := benchPublishCM(b, "bench-commit")
	face := cm.ExecutorConfig()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		st := cm.StageExecutor(cm.NewExecutorCandidate(face), face, nil)
		b.StartTimer()
		cm.ActivateExecutor(st) // the one linearization point, nothing else
	}
	b.StopTimer()
	if got := cm.ExecutorRefs(); got.PendingRetirees != 0 {
		b.Fatalf("an idle commit must reclaim its predecessor immediately: %+v", got)
	}
}

// BenchmarkLeaseAcquire 只计量「获取」：登记在途引用并取回执行器。释放在计时区外
// 完成——把 acquire/release 合成一个数字，就看不出热更期间真正被挡住的是哪一半。
func BenchmarkLeaseAcquire(b *testing.B) {
	cm := benchPublishCM(b, "bench-acquire")

	var held []*ExecLease
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		held = append(held, cm.AcquireLease(LeaseTurn))
	}
	b.StopTimer()
	for _, l := range held {
		l.Release()
	}
	if got := cm.ExecutorRefs(); got.InFlightTurns != 0 {
		b.Fatalf("every acquired lease must be released: %+v", got)
	}
}

// BenchmarkLeaseRelease 计量「释放」单独的成本：它是退役代真正被回收的触发点
// （回收本身另测），也是空闲态能否立刻收敛的关键。
func BenchmarkLeaseRelease(b *testing.B) {
	cm := benchPublishCM(b, "bench-release")

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		l := cm.AcquireLease(LeaseTurn) // setup: outside the measured span
		b.StartTimer()
		l.Release()
		b.StopTimer()
	}
	if got := cm.ExecutorRefs(); got.InFlightTurns != 0 {
		b.Fatalf("release loop must not leak references: %+v", got)
	}
}

// BenchmarkRetirementReclaim 度量「回收」这一具体 disposer：先在计时外造出一个
// 被引用保住的退役代，然后只计它被真正关闭的那一步。轮询式观测一律放在计时区外。
func BenchmarkRetirementReclaim(b *testing.B) {
	cm := benchPublishCM(b, "bench-reclaim")
	face := cm.ExecutorConfig()
	benchRun := func() *ExecLease { return cm.AcquireLease(LeaseTurn) }

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		keeper := benchRun()                                                            // pins the current generation
		cm.ActivateExecutor(cm.StageExecutor(cm.NewExecutorCandidate(face), face, nil)) // retires it, still held
		b.StartTimer()
		keeper.Release() // the measured span: this is where the retired generation dies
		b.StopTimer()
	}
	// The post-loop observation deliberately sits OUTSIDE every measured span.
	if got := cm.ExecutorRefs(); got.PendingRetirees != 0 || got.InFlightTurns != 0 {
		b.Fatalf("reclaim must finish without residue: %+v", got)
	}
}

// BenchmarkBeginTurn keeps the PAIRED cost (the actual per-turn hot path): acquire
// and release as production does them, with the reloader that returns early.
func BenchmarkBeginTurn(b *testing.B) {
	cm := benchPublishCM(b, "bench-begin-turn")
	cm.SetOrgReloader(func() { _ = time.Since(time.Now()) })

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exec, release := cm.BeginTurn()
		if exec == nil {
			b.Fatal("BeginTurn must return the effective executor")
		}
		release()
	}
	b.StopTimer()
	if refs := cm.ExecutorRefs(); refs.InFlightTurns != 0 {
		b.Fatalf("acquire/release must be paired; %d references leaked", refs.InFlightTurns)
	}
}

// BenchmarkRunFlowPinnedExecutor 度量带钉定执行器的 turn 入口开销（pinned 非 nil
// 时省掉一次 active 读，其余同形）。
func BenchmarkRunFlowPinnedExecutor(b *testing.B) {
	m := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("bench-runflow", m, nil, make(chan *event.Event, 4096), nil)
	b.Cleanup(func() { _ = cm.Close() })
	exec, release := cm.BeginTurn()
	defer release()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		if err := cm.RunFlowWithExecutor(ctx, model.NewUserMessage("bench"), exec); err != nil {
			b.Fatal(err)
		}
		cancel()
	}
}
