package memory

import (
	"errors"
	"testing"
)

// mockSink 记录上报（验证 ErrorTrackingStore 的归因与上报）。
type mockSink struct {
	failures  []string
	successes []string
}

func (m *mockSink) ReportFailure(dep string, _ error) { m.failures = append(m.failures, dep) }
func (m *mockSink) ReportSuccess(dep string)          { m.successes = append(m.successes, dep) }

// errStore 是可注入 StoreEvent 错误的 MemoryStore（嵌入 InMemoryStore 覆盖写路径）。
type errStore struct {
	*InMemoryStore
	storeErr error
}

func (e *errStore) StoreEvent(k int64, ev FullEvent) error {
	if e.storeErr != nil {
		return e.storeErr
	}
	return e.InMemoryStore.StoreEvent(k, ev)
}

func TestErrorTrackingStore_ClassifyAndReport(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantDep string
		isFail  bool
	}{
		{"fork/exec→rustviking", errors.New("fork/exec rustviking: no such file"), "rustviking", true},
		{"executable not found→rustviking", errors.New("executable file not found in $PATH"), "rustviking", true},
		{"ENOSPC→disk", errors.New("write: no space left on device"), "disk", true},
		{"disk quota→disk", errors.New("disk quota exceeded"), "disk", true},
		{"其余→memory", errors.New("segment corrupted"), "memory", true},
		{"成功→memory 恢复", nil, "memory", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &mockSink{}
			es := &errStore{InMemoryStore: NewInMemoryStore(), storeErr: tc.err}
			ets := NewErrorTrackingStore(es, sink)
			k := NewSnowflakeEventKey(1, testBaseMs)
			_ = ets.StoreEvent(k, FullEvent{EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe, Timestamp: testBaseMs})
			if tc.isFail {
				if len(sink.failures) != 1 || sink.failures[0] != tc.wantDep {
					t.Fatalf("失败应归因 %s, got failures=%v", tc.wantDep, sink.failures)
				}
			} else {
				// M2: 写成功上报存储栈三依赖恢复（memory+disk+rustviking），否则 disk/rustviking
				// 一旦 degraded 无恢复信号，卡到重启（违背三段式）。
				if len(sink.successes) != 3 {
					t.Fatalf("写成功应上报 memory+disk+rustviking 三依赖恢复(M2), got %v", sink.successes)
				}
			}
		})
	}
}

func TestErrorTrackingStore_NilSinkPassthrough(t *testing.T) {
	// sink nil → 纯透传，正常读写不 panic（配置门控关闭 = 现状）。
	inner := NewInMemoryStore()
	ets := NewErrorTrackingStore(inner, nil)
	k := NewSnowflakeEventKey(1, testBaseMs)
	if err := ets.StoreEvent(k, FullEvent{EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe, Timestamp: testBaseMs}); err != nil {
		t.Fatalf("透传 StoreEvent: %v", err)
	}
	got, err := ets.GetEvent(k)
	if err != nil || got == nil {
		t.Fatalf("透传 GetEvent: got=%v err=%v", got, err)
	}
}

func TestErrorTrackingStore_OptionalInterfacePassthrough(t *testing.T) {
	// 包裹 engineBridge（有 MemoryEngine）→ ErrorTrackingStore 必须透传 MemoryEngineProvider，
	// 否则 recall hybrid 断言 memStore.(MemoryEngineProvider) 失效（能力丢失）。
	inner := NewInMemoryStore()
	eng := NewInMemoryEngine(inner, NewMockEmbedder(16), EngineConfig{})
	defer eng.Close()
	bridge := NewEngineBridge(inner, eng)
	// 下游拿到的是 MemoryStore 接口（memStore），故赋给接口再断言可选能力穿透。
	var ms MemoryStore = NewErrorTrackingStore(bridge, nil)

	ep, ok := ms.(MemoryEngineProvider)
	if !ok || ep.MemoryEngine() == nil {
		t.Fatal("ErrorTrackingStore 应透传 MemoryEngineProvider（否则 recall hybrid 失效）")
	}
	// Close 透传（agent 引擎回收依赖）。
	c, ok := ms.(interface{ Close() error })
	if !ok {
		t.Fatal("ErrorTrackingStore 应透传 Close（agent.Closer 引擎回收）")
	}
	_ = c.Close()
}

func TestErrorTrackingStore_ReadPathReportsOnlyOnError(t *testing.T) {
	// 读路径成功不上报（读成功不代表写依赖恢复），失败上报。
	sink := &mockSink{}
	ets := NewErrorTrackingStore(NewInMemoryStore(), sink)
	// QueryEvents 成功 → 不上报。
	if _, err := ets.QueryEvents(QueryOptions{PartitionIDs: []int{1}}); err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(sink.failures) != 0 || len(sink.successes) != 0 {
		t.Fatalf("读成功不应上报, got failures=%v successes=%v", sink.failures, sink.successes)
	}
}

// TestErrorTrackingStore_VectorStubNotReported 是 S1 回归：未配引擎时 SearchByEmbedding 恒返回
// ErrVectorSearchNotSupported（能力声明），不应计为 rustviking 失败（否则未配语义检索的部署
// 一调用即退化）。
func TestErrorTrackingStore_VectorStubNotReported(t *testing.T) {
	sink := &mockSink{}
	ets := NewErrorTrackingStore(NewInMemoryStore(), sink)
	_, _ = ets.SearchByEmbedding([]float32{0.1, 0.2}, 5)
	if len(sink.failures) != 0 {
		t.Fatalf("能力 stub 错误(ErrVectorSearchNotSupported)不应上报失败(S1), got %v", sink.failures)
	}
}

// TestClassifyStoreErr_DiskBeforeRustviking 是 S3 回归：disk 特征先判 + rustviking 收窄到
// fork/exec（不用泛 "rustviking" 匹配）。
func TestClassifyStoreErr_DiskBeforeRustviking(t *testing.T) {
	// ENOSPC 内嵌 rustviking 字样 → 仍归 disk（disk 先判，防掩盖磁盘满根因）。
	if got := classifyStoreErr(errors.New("rustviking kv put: no space left on device")); got != depDisk {
		t.Fatalf("ENOSPC(含rustviking字样)应归 disk(S3), got %s", got)
	}
	// 纯 fork/exec → rustviking（CLI fork 失败确证）。
	if got := classifyStoreErr(errors.New("fork/exec /usr/bin/rustviking: permission denied")); got != depRustViking {
		t.Fatalf("fork/exec 应归 rustviking, got %s", got)
	}
	// 泛 rustviking 业务错误(无 fork/exec)→ memory（S3 收窄，不误归依赖退化）。
	if got := classifyStoreErr(errors.New("rustviking: index corrupted")); got != depMemory {
		t.Fatalf("泛 rustviking 业务错误应归 memory(S3收窄), got %s", got)
	}
}
