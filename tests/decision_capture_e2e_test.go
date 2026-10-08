package tagent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	trpcevent "trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	tagent "github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/config"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/rl"
)

// captureProbeTool 是一个只回答自己标记的可调用工具：子 agent 的真实工具往返由它承担，
// 返回值只有被构建的那个实例能产出，因此父回合收到的结果可反证是哪一代装的工具在服务。
type captureProbeTool struct {
	marker string
}

// Declaration 声明工具名与最小输入面。
func (p *captureProbeTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        "capture_probe",
		Description: "probe",
		InputSchema: &trpctool.Schema{Type: "object"},
	}
}

// Call 返回构造时钉住的标记。
func (p *captureProbeTool) Call(_ context.Context, _ []byte) (any, error) {
	return p.marker, nil
}

// registerCaptureProbe 只装一次工厂：重复注册同名 id 会 panic，测试因此可 -count=N 复跑。
var registerCaptureProbe sync.Once

func installCaptureProbe() {
	registerCaptureProbe.Do(func() {
		agent.RegisterPlainTool("capture_probe", func(agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
			return &captureProbeTool{marker: "PROBE-OK"}, nil
		})
	})
}

// captureRoundModel 是 mock 模型，但它服务的是真实管线：每个 agent 一个回合内的第一次调用
// 发起它当时真被声明的第一个工具，第二次调用收口。
//   - blankIDFor 命中的标签，其收口响应的 Response.ID 留空：精确键没有证据可依据；
//   - served 记录每次调用看到的系统提示前缀、被声明的工具名与给出的响应身份，供与 v2 记录对账。
type captureRoundModel struct {
	mu         sync.Mutex
	perCall    map[string]int
	served     []captureServed
	blankIDFor map[string]bool
}

// captureServed 是一次被服务的调用在模型边界上的可见事实。
type captureServed struct {
	label      string
	tools      []string
	responseID string
}

func captureLabelOf(req *model.Request) string {
	for _, msg := range req.Messages {
		if msg.Role == model.RoleSystem {
			if f := strings.Fields(msg.Content); len(f) > 0 {
				return f[0]
			}
		}
	}
	return ""
}

func captureToolNames(req *model.Request) []string {
	var names []string
	for _, t := range req.Tools {
		if d := t.Declaration(); d != nil {
			names = append(names, d.Name)
		}
	}
	return names
}

// GenerateContent 按上述脚本作答，并把当次调用的可见事实登记下来。
func (m *captureRoundModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	label := captureLabelOf(req)
	tools := captureToolNames(req)

	m.mu.Lock()
	if m.perCall == nil {
		m.perCall = map[string]int{}
	}
	m.perCall[label]++
	n := m.perCall[label]
	offer := ""
	if n == 1 && len(tools) > 0 {
		offer = tools[0]
	}
	resp := &model.Response{Done: true}
	if offer != "" {
		id := fmt.Sprintf("resp-%s-%d", label, n)
		resp.ID = id
		resp.Choices = []model.Choice{{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{
			Type: "function", ID: "call-" + offer,
			Function: model.FunctionDefinitionParam{Name: offer, Arguments: []byte(`{"request":"work"}`)},
		}}}}}
		m.served = append(m.served, captureServed{label: label, tools: tools, responseID: id})
	} else {
		content := "served:" + label
		if m.blankIDFor[label] {
			resp.Choices = []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}}
			m.served = append(m.served, captureServed{label: label, tools: tools, responseID: ""})
		} else {
			id := fmt.Sprintf("resp-%s-%d", label, n)
			resp.ID = id
			resp.Choices = []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}}
			m.served = append(m.served, captureServed{label: label, tools: tools, responseID: id})
		}
	}
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)
	ch <- resp
	close(ch)
	return ch, nil
}

// Info 报告 mock 身份。
func (m *captureRoundModel) Info() model.Info { return model.Info{Name: "capture-round"} }

// snapshotServed 返回被服务调用的副本。
func (m *captureRoundModel) snapshotServed() []captureServed {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]captureServed(nil), m.served...)
}

// captureRecordShape 是 v2 记录里本测试要读的部分（其余字段由采集层的合同文档定义）。
type captureRecordShape struct {
	SchemaVersion  int      `json:"schema_version"`
	RunID          string   `json:"run_id"`
	CallID         string   `json:"call_id"`
	CaptureScope   string   `json:"capture_scope"`
	BindingStatus  string   `json:"binding_status"`
	MissingReasons []string `json:"missing_reasons"`
	ResponseID     string   `json:"response_id"`
	RequestDigest  string   `json:"request_digest"`
	SessionID      string   `json:"session_id"`
	Owner          struct {
		CaptureNamespace string   `json:"capture_namespace"`
		PartitionID      string   `json:"partition_id"`
		Purpose          string   `json:"purpose"`
		AgentName        string   `json:"agent_name"`
		SessionID        string   `json:"session_id"`
		RootSessionID    string   `json:"root_session_id"`
		InvocationID     string   `json:"invocation_id"`
		InputEventKeys   []string `json:"input_event_keys"`
	} `json:"owner"`
	LLMCall struct {
		Request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"request"`
		TerminalStatus struct {
			Kind       string `json:"kind"`
			ResponseID string `json:"response_id"`
		} `json:"terminal_status"`
	} `json:"llm_call"`
}

// readCaptureRecords 读出目录里全部 v2 记录，并按所在文件返回，供"文件即会话"的断言使用。
func readCaptureRecords(t *testing.T, dir string) map[string][]captureRecordShape {
	t.Helper()
	out := map[string][]captureRecordShape{}
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
			continue
		}
		body, rerr := os.ReadFile(filepath.Join(dir, f.Name()))
		require.NoError(t, rerr)
		for _, line := range strings.Split(string(body), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var rec captureRecordShape
			require.NoErrorf(t, json.Unmarshal([]byte(line), &rec), "record line must be JSON: %s", line)
			out[f.Name()] = append(out[f.Name()], rec)
		}
	}
	return out
}

// countLines 统计目录里所有记录文件的行数。
func countLines(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	for _, recs := range readCaptureRecords(t, dir) {
		n += len(recs)
	}
	return n
}

// captureConfigFor 声明一条把轨迹与 v2 采集都装起来的单 agent / 双 agent 配置。
func captureConfigFor(dir, entryName string, subName string) tagent.Config {
	agents := map[string]tagent.AgentConfig{
		entryName: {
			SystemPrompt:      tagent.PromptConfig{Inline: "ENTRY-PROMPT answer briefly"},
			MaxToolIterations: 3,
			Memory:            tagent.MemoryConfig{Type: "memory"},
			Tools: []tagent.ToolRef{{
				Kind:        tagent.ToolKindAgent,
				AgentID:     subName,
				Description: "delegate the work",
				Async:       boolPtr(false),
			}},
		},
		subName: {
			SystemPrompt:      tagent.PromptConfig{Inline: "SUB-PROMPT answer briefly"},
			MaxToolIterations: 3,
			Memory:            tagent.MemoryConfig{Type: "memory"},
			Tools:             []tagent.ToolRef{{Kind: tagent.ToolKindTool, ID: "capture_probe"}},
		},
	}
	return tagent.Config{
		Entry:             entryName,
		Agents:            agents,
		TrajectoryDump:    true,
		TrajectoryDir:     dir,
		TrajectoryCapture: config.CaptureBlock{Enabled: true, MaxRecordBytes: 1 << 22},
	}
}

// boolPtr 给 async:false 这类"必须显式声明的假值"提供一个可写形态。
func boolPtr(v bool) *bool { return &v }

// allFacts 取回某个 agent 分区里已提交的全部事实原文。
func allFacts(t *testing.T, store memory.MemoryStore, agentName string) []memory.FullEvent {
	t.Helper()
	refs, err := store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{memory.PartitionIDFromName(agentName)},
		Limit:        200,
	})
	require.NoError(t, err)
	out := make([]memory.FullEvent, 0, len(refs))
	for _, ref := range refs {
		evt, gerr := store.GetEvent(ref.EventKey)
		require.NoErrorf(t, gerr, "已提交事实必须可取回 key=%d", ref.EventKey)
		require.NotNilf(t, evt, "已提交事实必须可取回 key=%d", ref.EventKey)
		out = append(out, *evt)
	}
	return out
}

// allRecords 摊平目录里的全部记录，按文件名与行序稳定排列。
func allRecords(t *testing.T, dir string) []captureRecordShape {
	t.Helper()
	byFile := readCaptureRecords(t, dir)
	names := make([]string, 0, len(byFile))
	for name := range byFile {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []captureRecordShape
	for _, name := range names {
		out = append(out, byFile[name]...)
	}
	return out
}

// captureOwnerGaps 把 owner 里空掉的字段映射到它在账本上应当具名的原因。
func captureOwnerGaps(rec captureRecordShape) map[string]string {
	gaps := map[string]string{}
	if rec.Owner.CaptureNamespace == "" {
		gaps["capture_namespace"] = rl.MissingOwnerCaptureNamespace
	}
	if rec.Owner.RootSessionID == "" {
		gaps["root_session_id"] = rl.MissingOwnerRootSessionID
	}
	if rec.Owner.AgentName == "" {
		gaps["agent_name"] = rl.MissingOwnerAgentName
	}
	if rec.Owner.InvocationID == "" {
		gaps["invocation_id"] = rl.MissingOwnerInvocationID
	}
	if rec.Owner.Purpose == "" {
		gaps["purpose"] = rl.MissingOwnerPurpose
	}
	return gaps
}

// captureDeclaredToolNames 返回记录里声明的工具名，顺序即 SDK 请求里的顺序。
func captureDeclaredToolNames(rec captureRecordShape) []string {
	var out []string
	for _, d := range rec.LLMCall.Request.Tools {
		out = append(out, d.Name)
	}
	return out
}

// captureDeclaredToolDescription 按名字取回记录里那条工具声明的描述原文。
func captureDeclaredToolDescription(rec captureRecordShape, name string) (string, bool) {
	for _, d := range rec.LLMCall.Request.Tools {
		if d.Name == name {
			return d.Description, true
		}
	}
	return "", false
}

// captureSystemPromptOf 返回记录里那次请求的 system 提示原文。
func captureSystemPromptOf(rec captureRecordShape) string {
	for _, msg := range rec.LLMCall.Request.Messages {
		if msg.Role == "system" {
			return msg.Content
		}
	}
	return ""
}

// captureResponseIDs 建立"SDK 返回过的响应身份 → 那次调用"的索引。
func captureResponseIDs(records []captureRecordShape) map[string]captureRecordShape {
	out := map[string]captureRecordShape{}
	for _, rec := range records {
		if rec.ResponseID != "" {
			out[rec.ResponseID] = rec
		}
	}
	return out
}

// TestDecisionCapture_EndToEnd 端到端钉住 v2 决策采集在真实框架管线上的采集忠实性：
//   - 采集目录与记录文件从创建起就是私有权限面；
//   - 入口与子 agent 各走一轮真实工具往返，记录带那次调用真被声明的工具集合与 owner 归因；
//   - call_id 只盖在有精确响应证据的已提交事实上，并与记录里的 call_id 一致；
//   - owner 里缺哪一样证据就具名登记哪一样；没有响应身份的调用报 unbound 并具名，绝不借邻居的身份；
//   - 两个 agent 的归因与工具声明不互串，两个会话的记录落在各自文件、封账只算自己的行数；
//   - FlushAndWait 在关闭后交回 complete+sealed 的封账，written 与磁盘行数相等。
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestDecisionCapture_EndToEnd(t *testing.T) {
	installCaptureProbe()

	dir := filepath.Join(t.TempDir(), "capture-e2e")
	m := &captureRoundModel{blankIDFor: map[string]bool{"SUB-PROMPT": true}}
	entry, err := tagent.New(captureConfigFor(dir, "capture-entry", "capture-sub"), tagent.WithModel(m))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), modeOf(t, dir), "采集目录从创建起就是私有的")

	out, err := entry.StartLoop("capture-user", "capture-sess-main")
	require.NoError(t, err)
	entry.InjectMessage(model.NewUserMessage("do the work"))
	drainUntilFinal(t, out, 60*time.Second)

	tr := entry.TrajectoryRecorder()
	require.NotNil(t, tr, "trajectory_dump 装上采集记录器")
	require.True(t, tr.CaptureEnabled(), "trajectory_capture.enabled 装出 v2 采集层")
	require.NoError(t, entry.Close())

	manifest, err := tr.FlushAndWait(context.Background())
	require.NoError(t, err)
	require.True(t, manifest.Sealed, "关闭后的封账必须是 sealed")
	require.True(t, manifest.Complete, "无丢失且已同步的运行才配得上 complete：%+v", manifest.CaptureStats)
	require.Equal(t, rl.CaptureStatusComplete, manifest.Status)
	require.NotEmpty(t, manifest.RunID)
	require.EqualValues(t, countLines(t, dir), manifest.Written,
		"封账 written 必须等于磁盘上的记录行数，否则账本与字节不是同一件事")
	require.Len(t, manifest.Files, 1, "一次运行只有一个会话，记录不得散落到别的文件")
	require.Equal(t, "capture-sess-main", manifest.Files[0].SessionID)
	require.EqualValuesf(t, manifest.Written, manifest.Files[0].Records,
		"文件级 records 与账本 written 必须同数：%s", manifest.Files[0].SessionID)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, de := range entries {
		if !de.IsDir() && strings.HasSuffix(de.Name(), ".jsonl") {
			require.Equal(t, os.FileMode(0o600), modeOf(t, filepath.Join(dir, de.Name())),
				"记录文件承载完整请求原文，权限面必须私有")
		}
	}

	records := allRecords(t, dir)
	require.Len(t, records, 4, "入口两次调用 + 子 agent 两次调用，一条不多一条不少")
	require.Len(t, m.snapshotServed(), 4, "模型边界服务过的调用数与记录数必须同源")

	seenCall := map[string]bool{}
	for _, rec := range records {
		require.Equal(t, 2, rec.SchemaVersion, "采集层写的是 v2 记录形状")
		require.Equal(t, rl.CaptureScopeSDKRequest, rec.CaptureScope)
		require.Equal(t, manifest.RunID, rec.RunID, "同一运行的记录共享同一个 run_id")
		require.NotEmpty(t, rec.CallID)
		require.Falsef(t, seenCall[rec.CallID], "一次调用一个 call_id，不得重复：%s", rec.CallID)
		seenCall[rec.CallID] = true
		require.NotEmpty(t, rec.Owner.AgentName, "每条记录都要说清是谁的调用")
		require.NotEmpty(t, rec.Owner.RootSessionID, "每条记录都要能回到发起它的会话")
		require.NotEmpty(t, rec.RequestDigest, "请求摘要让离线侧可比对同一次调用")
	}

	t.Run("every absent owner field is named instead of invented", func(t *testing.T) {
		for _, rec := range records {
			gaps := captureOwnerGaps(rec)
			for field, reason := range gaps {
				require.Containsf(t, rec.MissingReasons, reason,
					"%s 是空的，账本必须具名登记，而不是留一个含义不明的空位", field)
			}
			for _, reason := range rec.MissingReasons {
				if !strings.HasPrefix(reason, "owner.") {
					continue
				}
				named := false
				for _, want := range gaps {
					if want == reason {
						named = true
					}
				}
				require.Truef(t, named, "登记了 %s，可这个字段其实是有值的：%+v", reason, rec.Owner)
			}
		}
	})

	byAgent := map[string][]captureRecordShape{}
	for _, rec := range records {
		byAgent[rec.Owner.AgentName] = append(byAgent[rec.Owner.AgentName], rec)
	}
	require.Len(t, byAgent["capture-entry"], 2, "入口的调用不得被记到子 agent 名下")
	require.Len(t, byAgent["capture-sub"], 2, "子 agent 的调用不得被记到入口名下")
	for agentName, recs := range byAgent {
		own, foreign := "capture_probe", "capture-sub"
		if agentName == "capture-entry" {
			own, foreign = "capture-sub", "capture_probe"
		}
		for _, rec := range recs {
			require.Contains(t, captureSystemPromptOf(rec), labelOfAgent(agentName),
				"记录里的请求必须来自它自称的那个 agent（%s）", agentName)
			require.Containsf(t, captureDeclaredToolNames(rec), own,
				"记录里的工具声明必须是该 agent 那次调用真被声明的工具：%v", captureDeclaredToolNames(rec))
			require.NotContainsf(t, captureDeclaredToolNames(rec), foreign,
				"两个 agent 的工具声明不得互串：%v", captureDeclaredToolNames(rec))
		}
	}

	description, declared := captureDeclaredToolDescription(byAgent["capture-entry"][0], "capture-sub")
	require.True(t, declared, "子 agent 是以工具面被声明的，记录里必须能看到这条声明")
	require.Equal(t, "delegate the work", description,
		"声明里的描述必须来自真实装配的那条工具，而不是采集层自己造的占位")

	store := entry.MemStore()
	require.NotNil(t, store, "入口的存储面可达")
	facts := allFacts(t, store, "capture-entry")
	require.NotEmpty(t, facts, "入口的回合事实必须已提交")
	byResponseID := captureResponseIDs(records)

	stamped := 0
	for _, evt := range facts {
		callID := evt.Metadata[tagentevent.MetaKeyCallID]
		responseID := ""
		if evt.Response != nil {
			responseID = evt.Response.ID
		}
		if callID == "" {
			if responseID != "" {
				_, known := byResponseID[responseID]
				require.Falsef(t, known,
					"事实 %s 带着 SDK 返回过的响应身份却没盖 call_id，盖章面漏了这一条", responseID)
			}
			continue
		}
		rec, known := byResponseID[responseID]
		require.Truef(t, known, "事实上的 call_id %s 必须对应一次真被采集的调用，而不是凭猜测关联", callID)
		require.Equal(t, rec.CallID, callID, "票据上的 call_id 必须就是那次调用的 call_id")
		stamped++
	}
	require.GreaterOrEqual(t, stamped, 1, "至少一条已提交事实走完了精确关联的写入面")

	var unbound *captureRecordShape
	for i, rec := range records {
		for _, reason := range rec.MissingReasons {
			if reason == rl.MissingBindingNoResponseID {
				unbound = &records[i]
			}
		}
	}
	require.NotNil(t, unbound, "对照条件：没有响应身份的调用确实被采集下来了")
	require.Empty(t, unbound.ResponseID, "它本来就没有响应身份，记录也不得替它编一个")
	require.Equal(t, rl.BindingUnbound, unbound.BindingStatus,
		"没有响应证据的调用不得被标成已绑定")
	require.GreaterOrEqual(t, manifest.UnboundNoResponseID, int64(1), "账本按同一原因计数")
	for _, evt := range facts {
		require.NotEqualf(t, unbound.CallID, evt.Metadata[tagentevent.MetaKeyCallID],
			"未绑定调用的 call_id %s 绝不得出现在任何已提交事实上（那意味着猜中了最近的调用）", unbound.CallID)
	}

	var standalone *captureRecordShape
	for i, rec := range records {
		for _, reason := range rec.MissingReasons {
			if reason == rl.MissingBindingNoInvocation {
				standalone = &records[i]
			}
		}
	}
	require.NotNil(t, standalone, "对照条件：没有调用归属的调用也被如实采集")
	require.Equal(t, rl.BindingUnbound, standalone.BindingStatus,
		"缺调用归属时宁可记成独立一次调用，也不借邻座的调用身份")
	require.NotEmpty(t, standalone.CallID, "独立记录仍然要有自己的 call_id，否则离线侧无法对齐这一条")

	t.Run("two sessions keep separate files and separate ledgers", func(t *testing.T) {
		shared := filepath.Join(t.TempDir(), "two-sessions")
		runs := runTwoSessionCapture(t, shared)
		require.Len(t, runs, 2)
		require.NotEqual(t, runs[0], runs[1], "两次运行必须各有自己的 run_id")
		records := readCaptureRecords(t, shared)
		require.Contains(t, records, "sess-alpha.jsonl", "会话即文件名：一个会话的记录不得落进另一个会话的文件")
		require.Contains(t, records, "sess-beta.jsonl")
		for name, recs := range records {
			for _, rec := range recs {
				require.Equalf(t, name, rec.SessionID+".jsonl",
					"%s 里的记录必须写在以它自己的会话命名的文件里", name)
			}
		}
	})
}

// labelOfAgent 把 owner 的 agent 名映射回 mock 模型可辨认的系统提示前缀。
func labelOfAgent(agentName string) string {
	if agentName == "capture-entry" {
		return "ENTRY-PROMPT"
	}
	return "SUB-PROMPT"
}

// runTwoSessionCapture 在同一采集目录下用两个独立实例各跑一个会话，逐实例核对封账，
// 返回各次运行的 run_id。
func runTwoSessionCapture(t *testing.T, dir string) []string {
	t.Helper()
	var runs []string
	for _, session := range []string{"sess-alpha", "sess-beta"} {
		m := &captureRoundModel{}
		inst, err := tagent.New(captureConfigFor(dir, "capture-entry", "capture-sub"), tagent.WithModel(m))
		require.NoError(t, err)
		out, err := inst.StartLoop("capture-user", session)
		require.NoError(t, err)
		inst.InjectMessage(model.NewUserMessage("do the work"))
		drainUntilFinal(t, out, 60*time.Second)
		tr := inst.TrajectoryRecorder()
		require.NotNil(t, tr)
		require.NoError(t, inst.Close())

		manifest, err := tr.FlushAndWait(context.Background())
		require.NoError(t, err)
		require.Truef(t, manifest.Complete, "%s 的运行必须无丢失封账：%+v", session, manifest.CaptureStats)
		require.Truef(t, manifest.Sealed, "%s 的运行必须封账", session)

		mine := 0
		for _, rec := range allRecords(t, dir) {
			if rec.RunID != manifest.RunID {
				continue
			}
			mine++
			require.Equalf(t, session, rec.SessionID,
				"%s 的账本里混进了别的会话的记录：call_id=%s", session, rec.CallID)
		}
		require.EqualValuesf(t, mine, manifest.Written,
			"%s 的 written 只统计自己这次运行（跨实例不串账）", session)
		runs = append(runs, manifest.RunID)
	}
	return runs
}

// drainUntilFinal 排空输出通道直到终态响应出现。
func drainUntilFinal(t *testing.T, out <-chan *trpcevent.Event, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case evt, ok := <-out:
			if !ok {
				return
			}
			if evt.IsFinalResponse() {
				return
			}
		case <-deadline:
			t.Fatalf("no terminal response within %s", timeout)
		}
	}
}

// modeOf 返回路径的权限位。
func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	require.NoError(t, err)
	return fi.Mode().Perm()
}
