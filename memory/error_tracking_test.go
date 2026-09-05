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
		name     string
		err      error
		wantDep  string
		isFail   bool
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
				if len(sink.successes) != 1 || sink.successes[0] != tc.wantDep {
					t.Fatalf("成功应上报 %s 恢复, got successes=%v", tc.wantDep, sink.successes)
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
	ets := NewErrorTrackingStore(bridge, nil)

	ep, ok := ets.(MemoryEngineProvider)
	if !ok || ep.MemoryEngine() == nil {
		t.Fatal("ErrorTrackingStore 应透传 MemoryEngineProvider（否则 recall hybrid 失效）")
	}
	// Close 透传（agent 引擎回收依赖）。
	c, ok := ets.(interface{ Close() error })
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
