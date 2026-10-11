// 职责：config.go 只做「配置描述 + 装配」——从 JSON 读取 Config、校验 model/tool/skill/plugin
// 的 Registry 引用、热加载时经 state.Apply 做架构变更，并为某个 id 构造可替换的 BaseTAgent 壳：
// 绑定 ResolveTool（definition 允许集 + recall / agent:<id> 适配）、ModelCompletion（请求时组装
// ModelRequest 全字段并交 CallModel）与声明的插件。
// 未实现：YAML/文件监听/调度器、循环体、演进判定、任务状态、重试与失败自动恢复；Source 由输入
// 明确指定（默认 user），反思即自定义 Source 的输入，无专属控制器，也不做生产保护。
// 接线：state.go 提供 State/AgentState 与 Apply；AgentTask 提供 agent:<id> 委派；
// Memory 提供 recall，CallModel 记录轨迹，agent.go 实现消费循环。
package prototype

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// LoadConfig 读取 JSON 配置文档，只保证是合法 JSON；引用校验需要 Registry，见 ReloadConfig。
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ReloadConfig 热加载配置：解析后校验 model/tool/skill/plugin 引用，构造 resolve（经 buildSystem 把
// skill 内容与 tools 行固化成生效 prompt），再交 state.Apply 做架构级变更（同 id 复用现场、缺 id 回收）；
// 不做文件监听。返回被回收的 owner id。
func ReloadConfig(path string, state *State, registry Registry, evo Evolution) ([]string, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	if err := validateRegistry(cfg, registry); err != nil {
		return nil, err
	}
	resolve := func(def AgentConfig) string { return buildSystem(def, registry) }
	return state.Apply(cfg, resolve, evo)
}

// NewAgent 为 id 装配执行壳：只绑定函数字段与插件，不实现循环、演进或任务状态。模型实例、
// 工具适配与轨迹记录都在请求/解析时经 Registry 取得。deliver 为投递任务执行体（ToolFunc 形态）：
// EventReply 路由经 owner 任务表 Spawn 一条 deliver 任务执行它，成败以 ToolResult 通知回管线。
func NewAgent(id string, state *State, registry Registry, deliver ToolFunc) (*BaseTAgent, error) {
	owner, err := state.Agent(id)
	if err != nil {
		return nil, err
	}
	plugins, err := buildPlugins(owner, registry)
	if err != nil {
		return nil, err
	}
	agent := &BaseTAgent{
		State:   owner,
		Deliver: deliver,
		Compact: func() { CompactResults(owner) }, // 模型调用后折叠刚用过的结果。
		Plugins: plugins,
	}
	agent.Run = agent.DefaultRun
	agent.OnEvents = agent.DefaultOnEvents
	agent.ModelCompletion = func(ctx context.Context, inputs []string) (ModelOutput, error) {
		def := owner.Definition()
		req := ModelRequest{
			AgentID:      def.ID,
			ModelName:    def.Model,
			SystemPrompt: owner.SystemPrompt(), // 读 state 保存的生效值，请求期不再现拼 buildSystem。
			Params:       def.Params,
			Skills:       def.Skills,
			Tools:        def.Tools,
			Inputs:       inputs,
		}
		return CallModel(ctx, registry.Models[def.Model], req, registry.Record)
	}
	agent.ResolveTool = func(name string) ToolFunc {
		def := owner.Definition()
		if !declaresTool(def, name) {
			return nil
		}
		if name == "recall" {
			return func(ctx context.Context, args string) (string, error) {
				return owner.Memory.Recall(ctx, args)
			}
		}
		if child := strings.TrimPrefix(name, "agent:"); child != name {
			return AgentTask(state, child, registry)
		}
		return registry.Tools[name]
	}
	return agent, nil
}

// buildPlugins 为声明的插件名经 Registry 工厂创建实例（工厂接收 owner 以接结果外置等现场）。
func buildPlugins(owner *AgentState, registry Registry) ([]Plugin, error) {
	names := owner.Definition().Plugins
	plugins := make([]Plugin, 0, len(names))
	for _, name := range names {
		factory, ok := registry.Plugins[name]
		if !ok || factory == nil {
			return nil, fmt.Errorf("prototype: agent %q 引用未知 plugin %q", owner.ID, name)
		}
		plugins = append(plugins, factory(owner))
	}
	return plugins, nil
}

// buildSystem 组装 system：配置 prompt + 各 skill 内容 + 可用工具名称。
func buildSystem(def AgentConfig, registry Registry) string {
	var b strings.Builder
	b.WriteString(def.SystemPrompt)
	for _, name := range def.Skills {
		if content, ok := registry.Skills[name]; ok {
			b.WriteString("\n[skill:" + name + "] " + content)
		}
	}
	if len(def.Tools) > 0 {
		b.WriteString("\n[tools] " + strings.Join(def.Tools, ","))
	}
	return b.String()
}

// declaresTool 判定工具是否在当前 definition 允许集内。
func declaresTool(def AgentConfig, name string) bool {
	for _, t := range def.Tools {
		if t == name {
			return true
		}
	}
	return false
}

// validateRegistry 校验配置引用：model/skill/plugin 必须存在且非 nil，普通 tool 须能解析，
// recall 为内建，agent:<id> 的目标必须在同一配置中声明。
func validateRegistry(cfg Config, registry Registry) error {
	ids := make(map[string]bool, len(cfg.Agents))
	for _, def := range cfg.Agents {
		ids[def.ID] = true
	}
	for _, def := range cfg.Agents {
		if registry.Models[def.Model] == nil {
			return fmt.Errorf("prototype: agent %q 引用未知 model %q", def.ID, def.Model)
		}
		for _, s := range def.Skills {
			if _, ok := registry.Skills[s]; !ok {
				return fmt.Errorf("prototype: agent %q 引用未知 skill %q", def.ID, s)
			}
		}
		for _, p := range def.Plugins {
			if _, ok := registry.Plugins[p]; !ok || registry.Plugins[p] == nil {
				return fmt.Errorf("prototype: agent %q 引用未知 plugin %q", def.ID, p)
			}
		}
		for _, t := range def.Tools {
			if t == "recall" {
				continue
			}
			if child := strings.TrimPrefix(t, "agent:"); child != t {
				if !ids[child] {
					return fmt.Errorf("prototype: agent %q 引用未知子 agent %q", def.ID, child)
				}
				continue
			}
			if registry.Tools[t] == nil {
				return fmt.Errorf("prototype: agent %q 引用未知 tool %q", def.ID, t)
			}
		}
	}
	return nil
}
