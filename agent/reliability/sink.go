// 契约: docs/wiki/reliability/durable-delivery.md#degradation-ladder
package reliability

// MemorySink 把 DegradationManager 适配为 memory.DegradationSink（string 依赖名 → typed Dependency）。
type MemorySink struct {
	Mgr *DegradationManager
}

// ReportFailure 实现 memory.DegradationSink：string 依赖名转 typed Dependency 上报失败。
func (s MemorySink) ReportFailure(dep string, err error) {
	if s.Mgr != nil {
		s.Mgr.ReportFailure(Dependency(dep), err)
	}
}

// ReportSuccess 实现 memory.DegradationSink：string 依赖名转 typed Dependency 上报成功。
func (s MemorySink) ReportSuccess(dep string) {
	if s.Mgr != nil {
		s.Mgr.ReportSuccess(Dependency(dep))
	}
}
