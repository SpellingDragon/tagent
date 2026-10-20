package tagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	tagentfile "github.com/SpellingDragon/tagent/tool/file"

	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	toolskill "trpc.group/trpc-go/trpc-agent-go/tool/skill"
)

// supplier produces the native pieces of one agent's candidate binding on
// demand. Its factories run in the assembly order the construction chain fixes
// for one agent: the model first, then the native tools and their tool-set
// connection, then the file source, then the plugin registration units. Every
// factory reports the release that frees what it acquired beside the piece
// itself, so an acquisition is registered before the next factory runs. wrap is
// the admission wrap every native callable tool of the agent is registered
// under, so the contributed, file and skill families are admitted through one
// and the same registration wrapper.
//
// The plugins factory answers the ordered registration units of the agent and
// builds no pipeline here: the two-phase flow is assembled from them by the
// dispatch-time consumer (plugin.NewPipeline on the published binding), so the
// assembly phase owns construction and registration alone and never grows a
// second phase channel beside the published pipeline.
type supplier struct {
	model   func(context.Context, config.AgentSpec) (model.Model, func(context.Context) error, error)
	tools   func(context.Context, config.AgentSpec) ([]trpctool.Tool, trpctool.ToolSet, func(context.Context) error, error)
	prompt  func(context.Context, config.AgentSpec) (state.PromptSource, error)
	plugins func(context.Context, config.AgentSpec) ([]plugin.Plugin, error)
	wrap    func(trpctool.CallableTool) trpctool.Tool
}

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// prepare assembles the candidate live binding of every declared agent and
// hands the whole candidate to the state owner as one Candidate: the prepared
// StatePlan beside the resource ledger of what its construction acquired, so
// the publication entry frees exactly that ledger when the candidate leaves
// without taking effect. The native pieces of an agent are constructed in the
// assembly order the construction chain fixes, and every acquired resource is
// registered in the candidate resource ledger beside its release at the moment
// it is acquired.
//
// A construction that fails part way frees what the candidate acquired in the
// reverse of the acquisition order and once each, and keeps the failure that
// stopped it as the returned cause. The shared faces and the bindings already
// in service are candidates for nothing and are therefore left untouched.
func prepare(ctx context.Context, plan state.OrgPlan, suppliers map[event.AgentID]supplier) (state.Candidate, error) {
	if plan.Declaration == nil {
		return state.Candidate{}, state.ErrPlanMissing
	}
	leases := state.NewLeases()
	agents := map[event.AgentID]state.AgentBinding{}
	for _, spec := range plan.Declaration.Agents {
		id := event.AgentID(spec.ID)
		s, ok := suppliers[id]
		if !ok {
			return state.Candidate{}, abandon(ctx, leases, fmt.Errorf("tagent: no supplier constructs the candidate binding of agent %q", spec.ID))
		}
		binding, err := buildBinding(ctx, spec, s, leases)
		if err != nil {
			return state.Candidate{}, abandon(ctx, leases, err)
		}
		agents[id] = binding
	}
	built, err := state.NewStatePlan(plan, agents, leases.Refs())
	if err != nil {
		return state.Candidate{}, abandon(ctx, leases, err)
	}
	return state.Candidate{Plan: built, Resources: leases}, nil
}

// buildBinding constructs the candidate live binding of one declared agent in
// the assembly order and registers every acquired resource in the candidate
// resource ledger beside the release that frees it.
//
// The native file, skill and contributed tools — the function-tool shape among
// them — are registered before the binding is published, so the tools one
// request carries are exactly the registered ones and their load state is
// written back on the binding's own tool resources.
func buildBinding(ctx context.Context, spec config.AgentSpec, s supplier, leases *state.Leases) (state.AgentBinding, error) {
	native, releaseModel, err := s.model(ctx, spec)
	if err != nil {
		return state.AgentBinding{}, fmt.Errorf("tagent: construct the model of agent %q: %w", spec.ID, err)
	}
	leases.Register("model", spec.Model, releaseModel)
	tools, set, releaseSet, err := s.tools(ctx, spec)
	if err != nil {
		return state.AgentBinding{}, fmt.Errorf("tagent: construct the tools of agent %q: %w", spec.ID, err)
	}
	resources, registered, err := registerNativeTools(ctx, spec, tools, set, s.wrap)
	if err != nil {
		return state.AgentBinding{}, errors.Join(
			fmt.Errorf("tagent: register the native tools of agent %q: %w", spec.ID, err),
			releaseSet(ctx),
		)
	}
	leases.Register("toolset", spec.ID, func(ctx context.Context) error {
		return errors.Join(resources.Close(), releaseSet(ctx))
	})
	prompt, err := s.prompt(ctx, spec)
	if err != nil {
		return state.AgentBinding{}, fmt.Errorf("tagent: construct the file source of agent %q: %w", spec.ID, err)
	}
	leases.Register("filesource", spec.ID, nil)
	registrations, err := s.plugins(ctx, spec)
	if err != nil {
		return state.AgentBinding{}, fmt.Errorf("tagent: construct the plugins of agent %q: %w", spec.ID, err)
	}
	leases.Register("plugins", spec.ID, nil)
	return state.AgentBinding{Model: native, Tools: registered, Plugins: registrations, PromptSource: prompt, Set: resources}, nil
}

// registerNativeTools registers the native tool families of one declared agent
// on the binding's tool resources: the contributed native tools — the
// function-tool shape among them — beside the native file surface the agent's
// file face grants and the native skill load tool the declared skill files give
// a repository for. An agent that declares no file face carries no native file
// surface: a declaration is the authorization, and no declaration grants no
// file tool. Every native callable tool of every family is admitted through the
// same registration wrapper before it is registered, so a call of any family
// produces one real admission receipt and settles through the task face. The
// tools it returns are the registered ones in registration order, the exact
// list a request carries.
//
// A construction that fails is refused by name beside the family it was
// registering, so a binding is never published holding half a tool family.
func registerNativeTools(ctx context.Context, spec config.AgentSpec, contributed []trpctool.Tool, set trpctool.ToolSet, wrap func(trpctool.CallableTool) trpctool.Tool) (*state.ToolResources, []trpctool.Tool, error) {
	resources := state.NewToolResources("native-tools", set)
	registered := make([]trpctool.Tool, 0, len(contributed)+8)
	admitted, err := admitTools(contributed, wrap)
	if err != nil {
		return nil, nil, fmt.Errorf("admit the contributed tools: %w", err)
	}
	resources.Register(state.ToolKindFunction, "contributed", admitted...)
	registered = append(registered, admitted...)
	if surface, err := nativeFileSurface(spec); err != nil {
		return nil, nil, err
	} else if surface != nil {
		admitted, err := admitTools(withoutContributed(surface.Tools(ctx), contributed), wrap)
		if err != nil {
			surface.Close()
			return nil, nil, fmt.Errorf("admit the native file tools under %q: %w", surface.BaseDir(), err)
		}
		resources.Attach(surface)
		resources.Register(state.ToolKindFile, surface.BaseDir(), admitted...)
		registered = append(registered, admitted...)
	}
	if roots := skillRoots(spec.Files); len(roots) > 0 {
		repo, err := skill.NewFSRepository(roots...)
		if err != nil {
			return nil, nil, fmt.Errorf("open the framework skill repository under %q: %w", strings.Join(roots, ","), err)
		}
		loader := &skillLoadTool{inner: toolskill.NewLoadTool(repo), repo: repo, resources: resources}
		admitted, err := admitTools([]trpctool.Tool{loader}, wrap)
		if err != nil {
			return nil, nil, fmt.Errorf("admit the native skill load tool: %w", err)
		}
		resources.Register(state.ToolKindSkill, strings.Join(roots, ","), admitted...)
		registered = append(registered, admitted...)
	}
	return resources, registered, nil
}

// nativeFileSurface builds the file surface one agent declares: the file face
// fixes the base directory and the read roots, and the file ids the agent's
// references name fix the granted set. A declaration is the authorization: an
// agent that declares neither a face nor a file id carries no file surface and
// therefore no file tool, while a declared face that names no tool grants the
// whole frozen set over that face. An agent that declares a face or a file id
// but names no existing base directory is refused by name when the surface is
// built.
func nativeFileSurface(spec config.AgentSpec) (*tagentfile.Surface, error) {
	face := spec.File
	granted := declaredFileTools(spec)
	if face.BaseDir == "" && len(granted) == 0 && len(face.ReadRoots) == 0 {
		return nil, nil
	}
	surface, err := tagentfile.New(tagentfile.Options{
		BaseDir:   face.BaseDir,
		ReadRoots: face.ReadRoots,
		Tools:     granted,
	})
	if err != nil {
		return nil, fmt.Errorf("construct the native file tool set: %w", err)
	}
	return surface, nil
}

// declaredFileTools reports the file tool ids one agent's references name, in
// declaration order.
func declaredFileTools(spec config.AgentSpec) []string {
	names := tagentfile.Names()
	granted := make([]string, 0, len(names))
	for _, ref := range spec.Tools {
		for _, name := range names {
			if ref.Name == name {
				granted = append(granted, name)
			}
		}
	}
	return granted
}

// withoutContributed drops the native file tools a contributed tool already
// answers under the same name: the host contribution is the instance the model
// sees, so one name never reaches a request twice.
func withoutContributed(tools []trpctool.Tool, contributed []trpctool.Tool) []trpctool.Tool {
	declared := make(map[string]bool, len(contributed))
	for _, tool := range contributed {
		if declaration := tool.Declaration(); declaration != nil {
			declared[declaration.Name] = true
		}
	}
	out := make([]trpctool.Tool, 0, len(tools))
	for _, tool := range tools {
		if declaration := tool.Declaration(); declaration != nil && declared[declaration.Name] {
			continue
		}
		out = append(out, tool)
	}
	return out
}

// admitTools registers each native callable tool under the admission wrapper,
// so every tool family reaches a request only as an admitted tool. A tool that
// carries no native call surface is refused by name beside the family it was
// registering.
func admitTools(tools []trpctool.Tool, wrap func(trpctool.CallableTool) trpctool.Tool) ([]trpctool.Tool, error) {
	admitted := make([]trpctool.Tool, 0, len(tools))
	for _, tool := range tools {
		callable, ok := tool.(trpctool.CallableTool)
		if !ok {
			return nil, fmt.Errorf("tool %q carries no native call surface", toolName(tool))
		}
		admitted = append(admitted, wrap(callable))
	}
	return admitted, nil
}

// toolName reports the declared name of one tool for the messages that name
// the family a refusal belongs to.
func toolName(tool trpctool.Tool) string {
	if tool == nil {
		return ""
	}
	if declaration := tool.Declaration(); declaration != nil {
		return declaration.Name
	}
	return ""
}

// commonDir reports the deepest directory every declared file lives under, so
// the request-boundary file source reads the declared files under one root. A
// declaration that names no file carries no root.
func commonDir(files []config.FileRef) string {
	if len(files) == 0 {
		return ""
	}
	base := filepath.Dir(filepath.FromSlash(files[0].Path))
	for _, ref := range files[1:] {
		dir := filepath.Dir(filepath.FromSlash(ref.Path))
		for !within(dir, base) {
			parent := filepath.Dir(base)
			if parent == base {
				break
			}
			base = parent
		}
	}
	return base
}

// within reports whether the given path is the given directory or lives under
// it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

// skillRoots reports the skill repository roots the declared skill files live
// under: the folders that hold the skill folders themselves, in declaration
// order and once each. A reference points at a skill folder or at its SKILL.md
// file, and the framework repository indexes the skill folder under it.
func skillRoots(files []config.FileRef) []string {
	var roots []string
	seen := map[string]bool{}
	for _, ref := range files {
		if ref.Kind != config.FileKindSkill {
			continue
		}
		path := filepath.FromSlash(ref.Path)
		if filepath.Base(path) == skill.SkillFile {
			path = filepath.Dir(path)
		}
		root := filepath.Dir(path)
		if !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}
	return roots
}

// skillLoadTool is the request-boundary glue of the native skill load tool:
// the framework load tool's declaration beside the read that goes through the
// framework skill repository and fixes what it read on the binding's tool
// resources. The scanner and the parsing stay inside the framework, and the
// load state — the body and the content version it fixed — flows back into the
// state the binding carries.
type skillLoadTool struct {
	inner     *toolskill.LoadTool
	repo      skill.Repository
	resources *state.ToolResources
}

// Declaration returns the declaration of the native skill load tool.
func (t *skillLoadTool) Declaration() *trpctool.Declaration {
	return t.inner.Declaration()
}

// Call fixes the named skill at the request boundary it is called on and
// answers with the load it recorded: the first load of one skill in a request
// reads and fixes the body, the loads that follow in the same request answer
// from that fixed view, and the next request reads anew.
func (t *skillLoadTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var in struct {
		Skill string `json:"skill"`
	}
	if err := json.Unmarshal(jsonArgs, &in); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	if in.Skill == "" {
		return nil, errors.New("skill is required")
	}
	requestID, agent := "", ""
	if frame, ok := task.CallFrameFrom(ctx); ok {
		requestID = string(frame.Identity.DecisionID)
		agent = string(frame.Address.AgentID)
	}
	load, err := t.resources.FixateSkillLoad(requestID, agent, in.Skill, t.read)
	if err != nil {
		return nil, err
	}
	return fmt.Sprintf("loaded: %s", load.Skill), nil
}

// read reads one skill body through the framework skill repository at the
// request boundary and reports the content version it fixes.
func (t *skillLoadTool) read(name string) (state.SkillLoad, error) {
	if refreshable, ok := t.repo.(skill.RefreshableRepository); ok {
		if err := refreshable.Refresh(); err != nil {
			return state.SkillLoad{}, fmt.Errorf("refresh the skill repository: %w", err)
		}
	}
	got, err := t.repo.Get(name)
	if err != nil {
		return state.SkillLoad{}, fmt.Errorf("unknown skill: %s", name)
	}
	return state.SkillLoad{Body: got.Body, Version: skillContentVersion(got.Body)}, nil
}

// skillContentVersion reports the content version of one skill body fixed at
// a request boundary: the version names exactly that body and never a
// structural generation.
func skillContentVersion(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// abandon frees what one candidate acquisition accumulated beside the failure
// that stopped it and keeps that failure as the returned cause.
func abandon(ctx context.Context, leases *state.Leases, cause error) error {
	return errors.Join(cause, leases.ReleaseAll(ctx))
}
