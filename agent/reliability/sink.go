package reliability

// ==================== MemorySink（T-G · DegradationManager ↔ memory 依赖倒置适配）====================
//
// memory.ErrorTrackingStore 定义窄接口 memory.DegradationSink（用 string 依赖名），本适配器把
// DegradationManager（typed Dependency）桥接过去——memory 不 import reliability、reliability 不
// import memory（隐式满足接口），组装在 tagent wiring。消除底层 memory 依赖上层 reliability 的反向依赖。

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
