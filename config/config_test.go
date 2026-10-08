// 本文件负责配置模型的装载与校验判据：示例 YAML 的严格解析、默认配置的可构建性、生命周期字段投影。
// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
package config

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestValidate_AgentReferences(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		wantErr     bool
		errContains string
	}{
		{
			name: "valid agent tool reference",
			cfg: Config{
				Entry: "tagent",
				Agents: map[string]AgentConfig{
					"tagent": {
						Tools: []ToolRef{
							{Kind: ToolKindAgent, AgentID: "knowledge", DescriptionFile: "desc.md"},
						},
					},
					"knowledge": {},
				},
			},
			wantErr: false,
		},
		{
			name: "unknown agent reference",
			cfg: Config{
				Entry: "tagent",
				Agents: map[string]AgentConfig{
					"tagent": {
						Tools: []ToolRef{
							{Kind: ToolKindAgent, AgentID: "nonexistent", DescriptionFile: "desc.md"},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "unknown agent",
		},
		{
			name: "remote a2a reference needs no local definition",
			cfg: Config{
				Entry: "tagent",
				Agents: map[string]AgentConfig{
					"tagent": {
						Tools: []ToolRef{
							{Kind: ToolKindAgent, AgentID: "knowledge", Description: "k",
								Remote: &RemoteConfig{URL: "http://knowledge-service:8088"}},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "remote declaration without url is refused",
			cfg: Config{
				Entry: "tagent",
				Agents: map[string]AgentConfig{
					"tagent": {
						Tools: []ToolRef{
							{Kind: ToolKindAgent, AgentID: "knowledge", Description: "k",
								Remote: &RemoteConfig{URL: "   "}},
						},
					},
					"knowledge": {},
				},
			},
			wantErr:     true,
			errContains: "requires a url",
		},
		{
			name: "plain tool with valid id",
			cfg: Config{
				Entry: "tagent",
				Agents: map[string]AgentConfig{
					"tagent": {
						Tools: []ToolRef{
							{Kind: ToolKindTool, ID: "exec"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate tool id",
			cfg: Config{
				Entry: "tagent",
				Agents: map[string]AgentConfig{
					"tagent": {
						Tools: []ToolRef{
							{Kind: ToolKindTool, ID: "exec"},
							{Kind: ToolKindTool, ID: "exec"},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.ApplyDefaults()
			err := tt.cfg.Validate()

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidate_ArchitectureHierarchy(t *testing.T) {
	cfg := &Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {
				Tools: []ToolRef{
					{Kind: ToolKindAgent, AgentID: "action", DescriptionFile: "action_tool_desc.md"},
				},
			},
			"action": {
				Tools: []ToolRef{
					{Kind: ToolKindTool, ID: "read_file"},
					{Kind: ToolKindTool, ID: "save_file"},
					{Kind: ToolKindTool, ID: "list_file"},
					{Kind: ToolKindTool, ID: "search_file"},
					{Kind: ToolKindTool, ID: "search_content"},
					{Kind: ToolKindTool, ID: "read_multiple_files"},
					{Kind: ToolKindTool, ID: "replace_content"},
					{Kind: ToolKindTool, ID: "exec", DescriptionFile: "exec_tool_desc.md"},
				},
			},
		},
	}
	cfg.ApplyDefaults()
	err := cfg.Validate()
	require.NoError(t, err, "valid architecture should not produce error")
}

func TestDefaultConfig_KnowledgeTools(t *testing.T) {
	cfg := DefaultConfig()

	knowledgeAgent, ok := cfg.Agents["knowledge"]
	require.True(t, ok, "knowledge agent should exist in DefaultConfig")

	expectedTools := []string{
		"skill_search", "skill_load", "mcp_discover",
		"web_search", "duckduckgo_search", "memory_query",
	}

	require.Len(t, knowledgeAgent.Tools, len(expectedTools),
		"knowledge agent should have %d tool refs", len(expectedTools))

	for i, expectedID := range expectedTools {
		tr := knowledgeAgent.Tools[i]
		assert.Equal(t, ToolKindTool, tr.Kind, "tool[%d] should be kind=tool", i)
		assert.Equal(t, expectedID, tr.ID, "tool[%d] should have id=%q", i, expectedID)
	}
}

func TestDefaultConfig_RecallTools(t *testing.T) {
	cfg := DefaultConfig()

	recallAgent, ok := cfg.Agents["recall"]
	require.True(t, ok, "recall agent should exist in DefaultConfig")

	expectedTools := []string{
		"recall_query", "recall_get", "recall_recent", "recall_trace",
	}

	require.Len(t, recallAgent.Tools, len(expectedTools),
		"recall agent should have %d tool refs", len(expectedTools))

	for i, expectedID := range expectedTools {
		tr := recallAgent.Tools[i]
		assert.Equal(t, ToolKindTool, tr.Kind, "tool[%d] should be kind=tool", i)
		assert.Equal(t, expectedID, tr.ID, "tool[%d] should have id=%q", i, expectedID)
	}
}

func TestDefaultConfig_TagentTools(t *testing.T) {
	cfg := DefaultConfig()

	tagentAgent, ok := cfg.Agents["tagent"]
	require.True(t, ok, "tagent agent should exist in DefaultConfig")

	require.Len(t, tagentAgent.Tools, 3, "tagent should have 3 tools")

	assert.Equal(t, ToolKindAgent, tagentAgent.Tools[0].Kind)
	assert.Equal(t, "knowledge", tagentAgent.Tools[0].AgentID)

	assert.Equal(t, ToolKindAgent, tagentAgent.Tools[1].Kind)
	assert.Equal(t, "recall", tagentAgent.Tools[1].AgentID)

	assert.Equal(t, ToolKindTool, tagentAgent.Tools[2].Kind)
	assert.Equal(t, "exec", tagentAgent.Tools[2].ID)
}

func TestDefaultConfig_MeditationConfig(t *testing.T) {
	cfg := DefaultConfig()

	tagentAgent := cfg.Agents["tagent"]
	assert.False(t, tagentAgent.Meditation.Enabled,
		"DefaultConfig should not enable meditation by default")
}

func TestMeditationConfig_Fields(t *testing.T) {
	mc := MeditationConfig{
		Enabled:    true,
		Interval:   "30m",
		MinGap:     "2h",
		PromptFile: "meditation.md",
	}

	assert.True(t, mc.Enabled)
	assert.Equal(t, "30m", mc.Interval)
	assert.Equal(t, "2h", mc.MinGap)
	assert.Equal(t, "meditation.md", mc.PromptFile)
}

func TestLoadConfig_ExampleYAML(t *testing.T) {
	cfg, err := LoadConfig("../examples/wechat-bot/tagent.yaml")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	actionCfg, ok := cfg.Agents["action"]
	require.True(t, ok, "action agent should exist")

	var fileToolIDs []string
	for _, tr := range actionCfg.Tools {
		if tr.Kind == ToolKindTool {
			fileToolIDs = append(fileToolIDs, tr.ID)
		}
	}
	assert.Contains(t, fileToolIDs, "read_file")
	assert.Contains(t, fileToolIDs, "save_file")
	assert.NotContains(t, cfg.Agents, "read")
	assert.NotContains(t, cfg.Agents, "write")
}

// TestAgentConfig_TaskTerminalTTL pins that the task_terminal_ttl YAML field parses as a duration string and flows through to agent.TagentConfig.
// - It bounds the resume_task window for terminal subagent tasks; an unset field stays empty instead of gaining an implicit default.
func TestAgentConfig_TaskTerminalTTL(t *testing.T) {
	var acfg AgentConfig
	require.NoError(t, yaml.Unmarshal([]byte("task_terminal_ttl: \"30m\"\n"), &acfg))
	assert.Equal(t, "30m", acfg.TaskTerminalTTL)

	ttl, err := time.ParseDuration(acfg.TaskTerminalTTL)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, ttl)

	// Unset stays empty → buildAgent leaves TagentConfig.TaskTerminalTTL zero
	// → task package default (2m).
	var empty AgentConfig
	require.NoError(t, yaml.Unmarshal([]byte("max_tokens: 1000\n"), &empty))
	assert.Empty(t, empty.TaskTerminalTTL)
}

// TestResolveLifecycleConfig pins how YAML-declared lifecycle fields merge with the built-in defaults.
// - Declared fields override, unset fields fall back, and a negative global TTL disables TTL-based forgetting entirely.
// - Unspecified TypeTTL entries keep the built-in table; an invalid check interval keeps the 1h default.
func TestResolveLifecycleConfig(t *testing.T) {
	cfg := ResolveLifecycleConfig(nil)
	assert.Equal(t, 7, cfg.GlobalTTLDays)
	assert.Equal(t, 0, cfg.MaxEventsPerPartition)

	ttl, maxEv := 30, 50000
	cfg = ResolveLifecycleConfig(&LifecycleConfig{
		GlobalTTLDays:         &ttl,
		TypeTTL:               map[string]int{"thinking_plan": 1},
		CheckInterval:         "15m",
		MaxEventsPerPartition: &maxEv,
	})
	assert.Equal(t, 30, cfg.GlobalTTLDays)
	assert.Equal(t, 1, cfg.TypeTTL["thinking_plan"])
	assert.Equal(t, 30, cfg.TypeTTL["external_input"], "unspecified types keep default table")
	assert.Equal(t, 50000, cfg.MaxEventsPerPartition)
	assert.Equal(t, int64(15*60), int64(cfg.CheckInterval.Seconds()))

	off := -1
	cfg = ResolveLifecycleConfig(&LifecycleConfig{
		GlobalTTLDays: &off,
		CheckInterval: "not-a-duration",
	})
	assert.Equal(t, -1, cfg.GlobalTTLDays)
	assert.Equal(t, int64(3600), int64(cfg.CheckInterval.Seconds()), "invalid interval keeps 1h default")
}

// TestLifecycleConfigYAML pins that the lifecycle block parses from YAML into MemoryConfig.
// - The YAML field names are the contract users write in tagent.yaml, so optionality is asserted through pointers, not only through values.
func TestLifecycleConfigYAML(t *testing.T) {
	yamlSrc := `
entry: a
model: m
providers:
  p:
    api_endpoint: "http://x"
agents:
  a:
    memory:
      type: localfile
      path: /tmp/x
      lifecycle:
        global_ttl_days: 30
        type_ttl:
          thinking_plan: 1
        check_interval: "15m"
        max_events_per_partition: 50000
`
	tmp := t.TempDir()
	path := tmp + "/tagent.yaml"
	require.NoError(t, os.WriteFile(path, []byte(yamlSrc), 0o644))
	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	lc := cfg.Agents["a"].Memory.Lifecycle
	require.NotNil(t, lc)
	require.NotNil(t, lc.GlobalTTLDays)
	assert.Equal(t, 30, *lc.GlobalTTLDays)
	assert.Equal(t, 1, lc.TypeTTL["thinking_plan"])
	assert.Equal(t, "15m", lc.CheckInterval)
	require.NotNil(t, lc.MaxEventsPerPartition)
	assert.Equal(t, 50000, *lc.MaxEventsPerPartition)
}

// captureTestConfig builds a minimal loadable Config carrying one capture block,
// with defaults already applied — the shape Validate() sees at startup.
func captureTestConfig(dump bool, block CaptureBlock) *Config {
	cfg := &Config{
		Entry:             "tagent",
		Agents:            map[string]AgentConfig{"tagent": {}},
		TrajectoryDump:    dump,
		TrajectoryCapture: block,
	}
	cfg.ApplyDefaults()
	return cfg
}

// TestValidate_CompressSummaryTimeout pins the startup contract of compress.summary_timeout_seconds.
//   - 0 keeps the compress package default; a positive value travels verbatim
//   - a negative value fails the load with a named error instead of "no limit"
func TestValidate_CompressSummaryTimeout(t *testing.T) {
	compressTestConfig := func(timeout int) *Config {
		cfg := &Config{
			Entry:  "tagent",
			Agents: map[string]AgentConfig{"tagent": {Compress: CompressConfig{SummaryTimeoutSeconds: timeout}}},
		}
		cfg.ApplyDefaults()
		return cfg
	}

	require.NoError(t, compressTestConfig(0).Validate(), "0 must keep the compress default rather than declare a limit")
	require.NoError(t, compressTestConfig(1).Validate())
	require.NoError(t, compressTestConfig(120).Validate())
	require.ErrorIs(t, compressTestConfig(121).Validate(), ErrSummaryTimeoutTooLarge,
		"above the ceiling must be a named refusal, never a silent clamp")

	err := compressTestConfig(-1).Validate()
	require.Error(t, err, "a negative summary timeout must be rejected, not silently accepted")
	assert.ErrorIs(t, err, ErrSummaryTimeoutNegative)
	assert.Contains(t, err.Error(), "summary_timeout_seconds")
	assert.Contains(t, err.Error(), "tagent", "the failing agent must be named")

	neg := compressTestConfig(-5)
	assert.ErrorIs(t, neg.Validate(), ErrSummaryTimeoutNegative)
	assert.Equal(t, -5, neg.Agents["tagent"].Compress.SummaryTimeoutSeconds,
		"rejection must not rewrite the declared value")
}

// TestApplyDefaults_TrajectoryCaptureKeepsDeclaredValues pins that config is not the source of the capture limits.
//   - unset limits stay zero, so the capture layer's own normalize supplies them
//   - declared values survive ApplyDefaults untouched
func TestApplyDefaults_TrajectoryCaptureKeepsDeclaredValues(t *testing.T) {
	enabled := captureTestConfig(true, CaptureBlock{Enabled: true})
	assert.Equal(t, CaptureBlock{Enabled: true}, enabled.TrajectoryCapture,
		"unset limits must stay zero; copying defaults here would create a second source of truth")
	require.NoError(t, enabled.Validate())

	declared := captureTestConfig(true, CaptureBlock{
		Enabled:         true,
		MaxRecordBytes:  1 << 20,
		MaxPendingBytes: 2 << 20,
		MaxRunBytes:     3 << 20,
		MaxOpenFiles:    4,
	})
	assert.Equal(t, CaptureBlock{
		Enabled:         true,
		MaxRecordBytes:  1 << 20,
		MaxPendingBytes: 2 << 20,
		MaxRunBytes:     3 << 20,
		MaxOpenFiles:    4,
	}, declared.TrajectoryCapture)
	require.NoError(t, declared.Validate())
}

// TestValidate_TrajectoryCaptureRequiresDump pins that the capture layer only exists on top of a trajectory dump.
//   - enabled without trajectory_dump is a named refusal at Validate
//   - enabled with trajectory_dump validates clean
func TestValidate_TrajectoryCaptureRequiresDump(t *testing.T) {
	err := captureTestConfig(false, CaptureBlock{Enabled: true}).Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCaptureRequiresDump)
	assert.Contains(t, err.Error(), "trajectory_dump")

	require.NoError(t, captureTestConfig(true, CaptureBlock{Enabled: true}).Validate())
	require.NoError(t, captureTestConfig(false, CaptureBlock{}).Validate(),
		"a disabled capture block needs no trajectory_dump")
	require.NoError(t, captureTestConfig(false, CaptureBlock{MaxRecordBytes: 1024}).Validate(),
		"declared-but-unused limits stay acceptable while capture is off")
}

// TestValidate_TrajectoryCaptureNegativeLimits pins that a negative bound is a named startup failure per field.
//   - a negative limit is never reinterpreted as "unlimited"
//   - it is refused even while the block is off, so a typo is never ignored
func TestValidate_TrajectoryCaptureNegativeLimits(t *testing.T) {
	cases := []struct {
		name      string
		block     CaptureBlock
		wantField string
	}{
		{"max_record_bytes", CaptureBlock{Enabled: true, MaxRecordBytes: -1}, "max_record_bytes"},
		{"max_pending_bytes", CaptureBlock{Enabled: true, MaxPendingBytes: -1}, "max_pending_bytes"},
		{"max_run_bytes", CaptureBlock{Enabled: true, MaxRunBytes: -1}, "max_run_bytes"},
		{"max_open_files", CaptureBlock{Enabled: true, MaxOpenFiles: -1}, "max_open_files"},
		{"negative while capture is off", CaptureBlock{MaxPendingBytes: -1}, "max_pending_bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := captureTestConfig(true, tc.block).Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrCaptureNegativeLimit)
			assert.Contains(t, err.Error(), tc.wantField)
			assert.Contains(t, err.Error(), "-1", "the offending value must be reported")
		})
	}
}

// TestValidate_TrajectoryCaptureOpenFilesCeiling pins that the assembly surface rejects an above-ceiling bound.
//   - above the ceiling is a startup error, not a silent clamp; the library's clamp only covers in-library use
//   - within the ceiling it validates clean
func TestValidate_TrajectoryCaptureOpenFilesCeiling(t *testing.T) {
	require.NoError(t, captureTestConfig(true, CaptureBlock{Enabled: true, MaxOpenFiles: 1}).Validate())
	require.NoError(t, captureTestConfig(true, CaptureBlock{Enabled: true, MaxOpenFiles: MaxCaptureOpenFiles}).Validate())

	cfg := captureTestConfig(true, CaptureBlock{Enabled: true, MaxOpenFiles: MaxCaptureOpenFiles + 1})
	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCaptureOpenFilesExceeded)
	assert.Contains(t, err.Error(), "max_open_files")
	assert.Equal(t, MaxCaptureOpenFiles+1, cfg.TrajectoryCapture.MaxOpenFiles,
		"an above-ceiling declaration must stay visible, never be silently clamped")
}

// TestLoadConfig_NewKeys pins that both new keys load from YAML into their declared homes.
//   - the summary timeout stays inside the per-agent structural fingerprint
//   - the hot-reload snapshot round-trips them
//   - a misspelled new key fails the load instead of being ignored
func TestLoadConfig_NewKeys(t *testing.T) {
	const yamlSrc = `
entry: a
model: m
trajectory_dump: true
trajectory_capture:
  enabled: true
  max_record_bytes: 1048576
  max_pending_bytes: 8388608
  max_run_bytes: 33554432
  max_open_files: 8
agents:
  a:
    compress:
      summary_timeout_seconds: 8
`
	const typoSrc = `
entry: a
model: m
trajectory_dump: true
trajectory_capture:
  enabled: true
  max_rekord_bytes: 1048576
agents:
  a: {}
`
	const typoTopSrc = `
entry: a
model: m
trajectory_captur:
  enabled: true
agents:
  a: {}
`
	tmp := t.TempDir()
	write := func(name, src string) string {
		p := tmp + "/" + name
		require.NoError(t, os.WriteFile(p, []byte(src), 0o644))
		return p
	}

	cfg, err := LoadConfig(write("good.yaml", yamlSrc))
	require.NoError(t, err)
	assert.Equal(t, CaptureBlock{
		Enabled:         true,
		MaxRecordBytes:  1 << 20,
		MaxPendingBytes: 8 << 20,
		MaxRunBytes:     32 << 20,
		MaxOpenFiles:    8,
	}, cfg.TrajectoryCapture)
	assert.Equal(t, 8, cfg.Agents["a"].Compress.SummaryTimeoutSeconds)

	clone, err := cfg.Clone()
	require.NoError(t, err)
	assert.Equal(t, cfg.TrajectoryCapture, clone.TrajectoryCapture,
		"json/yaml tags must stay symmetric for the hot-reload snapshot")
	assert.Equal(t, 8, clone.Agents["a"].Compress.SummaryTimeoutSeconds)

	_, err = LoadConfig(write("typo.yaml", typoSrc))
	require.Error(t, err, "a misspelled key inside trajectory_capture must fail the load")
	assert.Contains(t, err.Error(), "max_rekord_bytes")

	_, err = LoadConfig(write("typo_top.yaml", typoTopSrc))
	require.Error(t, err, "a misspelled trajectory_capture block name must fail the load")
	assert.Contains(t, err.Error(), "trajectory_captur")
}

// TestMeditationConfig_ExtFields 钉住冥想扩字段的 YAML/JSON 装载面。
func TestMeditationConfig_ExtFields(t *testing.T) {
	var zero MeditationConfig
	require.NoError(t, yaml.Unmarshal([]byte("enabled: true\n"), &zero))
	assert.Empty(t, zero.ObservedNamespaces, "observed_namespaces must default empty")
	assert.Empty(t, zero.DeliverTo, "deliver_to must default empty (fail-closed)")

	var mc MeditationConfig
	require.NoError(t, yaml.Unmarshal([]byte(
		"observed_namespaces:\n  - recall\n  - \"session:42\"\ndeliver_to:\n  - entry\n"), &mc))
	assert.Equal(t, []string{"recall", "session:42"}, mc.ObservedNamespaces,
		"explicit observed_namespaces parses into the slice")
	assert.Equal(t, []string{"entry"}, mc.DeliverTo,
		"explicit deliver_to parses into the slice")

	var jm MeditationConfig
	require.NoError(t, json.Unmarshal(
		[]byte(`{"observed_namespaces":["a"],"deliver_to":["b","c"]}`), &jm))
	assert.Equal(t, []string{"a"}, jm.ObservedNamespaces,
		"JSON face reads the same keys (omitempty only affects marshalling)")
	assert.Equal(t, []string{"b", "c"}, jm.DeliverTo)

	b, err := json.Marshal(MeditationConfig{Enabled: true})
	require.NoError(t, err)
	assert.NotContains(t, string(b), "observed_namespaces",
		"unset fields are omitted so the off state carries no noise keys")
	assert.NotContains(t, string(b), "deliver_to")
}
