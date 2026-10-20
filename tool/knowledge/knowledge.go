// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
//
// Package knowledge is the model-facing tool surface of a knowledge agent: the
// skill search over the deployment's skill repository, the skill load that puts
// one skill's body in context, and the bounded file read inside one root. The
// three tools own no repository and no file system of their own: the skill face
// is the tRPC skill repository and the file face is the tRPC file tool set, so
// this package only adapts them onto the declaration the model reads.
package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/skill"
	toolskill "trpc.group/trpc-go/trpc-agent-go/tool/skill"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	toolfile "trpc.group/trpc-go/trpc-agent-go/tool/file"
)

// SkillSearchToolName, SkillLoadToolName and ReadFileToolName are the tool
// names the knowledge prompt and the declaration share. They are frozen: a
// rename is a prompt change and a declaration change in one breath.
const (
	// SkillSearchToolName is the name of the skill search tool.
	SkillSearchToolName = "skill_search"
	// SkillLoadToolName is the name of the skill load tool.
	SkillLoadToolName = "skill_load"
	// ReadFileToolName is the name of the bounded file read tool.
	ReadFileToolName = "read_file"
)

// defaultSearchLimit and maxSearchLimit bound how many skills one search
// answers with: a search is a shortlist, not the repository.
const (
	defaultSearchLimit = 10
	maxSearchLimit     = 50
)

// ErrSkillsMissing reports a surface assembled beside no skill repository: the
// search and the load refuse by name instead of answering an empty list.
var ErrSkillsMissing = errors.New("knowledge: the deployment wires no skill repository")

// ErrReadRootMissing reports a surface assembled beside no read root.
var ErrReadRootMissing = errors.New("knowledge: the deployment wires no read root")

// ErrFileToolMissing reports a tRPC file tool set that carries no read_file
// tool, so the declared name would answer nothing.
var ErrFileToolMissing = errors.New("knowledge: the file tool set carries no read file tool")

// Options fixes the faces one knowledge surface is assembled over: the skill
// repository the deployment holds and the root the file read is confined to.
type Options struct {
	// Skills is the deployment's skill repository; a surface beside none
	// refuses the two skill tools by name.
	Skills skill.Repository
	// ReadRoot is the directory every file read is confined to; a surface
	// beside none refuses the read by name.
	ReadRoot string
}

// New returns the knowledge surface over the faces the caller fixes, in a fixed
// order: skill_search, skill_load, read_file. A face the deployment leaves
// unwired contributes no tool, and a surface beside neither face returns none.
func New(ctx context.Context, opts Options) ([]trpctool.CallableTool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]trpctool.CallableTool, 0, 3)
	if opts.Skills != nil {
		out = append(out, &skillSearchTool{skills: opts.Skills})
		out = append(out, toolskill.NewLoadTool(opts.Skills))
	}
	if strings.TrimSpace(opts.ReadRoot) != "" {
		read, err := readFileTool(ctx, opts.ReadRoot)
		if err != nil {
			return nil, err
		}
		out = append(out, read)
	}
	return out, nil
}

// readFileTool builds the read face of one deployment: the tRPC file tool set
// over the declared root with every write and every wider search disabled, so
// the knowledge agent reads and never rewrites what it reads.
func readFileTool(ctx context.Context, root string) (trpctool.CallableTool, error) {
	set, err := toolfile.NewToolSet(
		toolfile.WithBaseDir(root),
		toolfile.WithSaveFileEnabled(false),
		toolfile.WithReplaceContentEnabled(false),
		toolfile.WithListFileEnabled(false),
		toolfile.WithSearchFileEnabled(false),
		toolfile.WithSearchContentEnabled(false),
	)
	if err != nil {
		return nil, fmt.Errorf("knowledge: open the read root %q: %w", root, err)
	}
	defer func() { _ = set.Close() }()
	for _, tool := range set.Tools(ctx) {
		declaration := tool.Declaration()
		if declaration == nil || declaration.Name != ReadFileToolName {
			continue
		}
		callable, ok := tool.(trpctool.CallableTool)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrFileToolMissing, ReadFileToolName)
		}
		return callable, nil
	}
	return nil, fmt.Errorf("%w: %q", ErrFileToolMissing, ReadFileToolName)
}

// searchArgs is the declared argument shape of one skill search.
type searchArgs struct {
	// Query is the text one search matches against the skill names and
	// descriptions; an empty query lists the shortlist as it stands.
	Query string `json:"query"`
	// Limit bounds how many skills the answer carries.
	Limit int `json:"limit"`
}

// searchHit is one skill a search answered with.
type searchHit struct {
	// Name is the skill name the load tool takes.
	Name string `json:"name"`
	// Description is the skill description the repository holds.
	Description string `json:"description"`
}

// searchAnswer is the answer of one skill search.
type searchAnswer struct {
	// Tool is the name of the tool that answered.
	Tool string `json:"tool"`
	// Count is how many skills the answer carries.
	Count int `json:"count"`
	// Skills is the shortlist, best match first.
	Skills []searchHit `json:"skills"`
}

// skillSearchTool is the model-facing skill search: it reads the repository the
// deployment already holds and answers the summaries that match, so no second
// index of the skills exists beside that repository.
type skillSearchTool struct {
	skills skill.Repository
}

// The search tool satisfies the native callable tool port.
var _ trpctool.CallableTool = (*skillSearchTool)(nil)

// ErrSearchArguments reports a search argument object that does not decode.
var ErrSearchArguments = errors.New("knowledge: the search arguments do not decode")

// Declaration reports the skill search declaration.
func (t *skillSearchTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name: SkillSearchToolName,
		Description: "从本部署的技能仓库里按名字与描述检索技能：要判断某个任务有没有现成技能可用时先用它。" +
			"结果是一张短名单（名字＋描述），名字可以直接交给 " + SkillLoadToolName + " 载入正文；不返回技能正文。",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"query": {
					Type:        "string",
					Description: "检索词，按名字与描述做不区分大小写的包含匹配；留空则按仓库顺序列出短名单",
				},
				"limit": {
					Type:        "integer",
					Description: "最多返回几条，缺省 10，最大 50",
				},
			},
		},
	}
}

// Call answers one skill search over the repository summaries. The matching is
// a rule and not a retrieval: every summary the terms appear in is answered, in
// repository order, cut to the page the call names.
func (t *skillSearchTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var args searchArgs
	if len(jsonArgs) > 0 {
		if err := json.Unmarshal(jsonArgs, &args); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSearchArguments, err)
		}
	}
	if t.skills == nil {
		return nil, fmt.Errorf("%s: %w", SkillSearchToolName, ErrSkillsMissing)
	}
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(args.Query)))
	limit := clampLimit(args.Limit)
	answer := searchAnswer{Tool: SkillSearchToolName, Skills: make([]searchHit, 0, limit)}
	for _, summary := range t.skills.Summaries() {
		if !matches(summary, terms) {
			continue
		}
		answer.Skills = append(answer.Skills, searchHit{Name: summary.Name, Description: summary.Description})
		if len(answer.Skills) >= limit {
			break
		}
	}
	answer.Count = len(answer.Skills)
	encoded, err := json.Marshal(answer)
	if err != nil {
		return nil, fmt.Errorf("%s: encode the answer: %w", SkillSearchToolName, err)
	}
	return string(encoded), nil
}

// matches reports whether every term of one search appears in the name or the
// description of one summary. A search that names no term matches every skill,
// so an empty query lists the shortlist as it stands.
func matches(summary skill.Summary, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	haystack := strings.ToLower(summary.Name + " " + summary.Description)
	for _, term := range terms {
		if !strings.Contains(haystack, term) {
			return false
		}
	}
	return true
}

// clampLimit resolves the page bound one search runs under: the default when it
// names none, the ceiling when it names more than one page may carry.
func clampLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultSearchLimit
	case limit > maxSearchLimit:
		return maxSearchLimit
	default:
		return limit
	}
}
