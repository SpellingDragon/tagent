// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
//
// The preset tools of the configuration face live beside the sub-agent tool
// because they share one composition-root seam: a declared `knowledge`
// reference resolves onto the trpc knowledge tool over a loaded source, a
// declared `recall` reference wraps the run's own memory retrieval face, and a
// declared `call_subagent` reference resolves onto the tool of this package.
// This file holds no retrieval of its own: the knowledge stack is the trpc
// one, the recall behaviour is the face the caller injects, and an unknown
// reference is left to the host-contributed tools instead of being refused.
package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/search"

	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/embedder"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	dirsource "trpc.group/trpc-go/trpc-agent-go/knowledge/source/dir"
	filesource "trpc.group/trpc-go/trpc-agent-go/knowledge/source/file"
	knowledgetool "trpc.group/trpc-go/trpc-agent-go/knowledge/tool"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/inmemory"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// KnowledgeRef, RecallRef and CallSubagentRef are the reference names a
// declaration uses to ask for a preset tool.
const (
	// KnowledgeRef is the reference resolving onto the trpc knowledge tool.
	KnowledgeRef = "knowledge"
	// RecallRef is the reference resolving onto the run's memory retrieval.
	RecallRef = "recall"
	// CallSubagentRef is the reference resolving onto the call_subagent tool.
	CallSubagentRef = ToolName
)

// CodeSearchToolName and KnowledgeSearchToolName are the tool names a knowledge
// reference may select through its "tool" parameter.
const (
	// KnowledgeSearchToolName is the tool name the plain knowledge search answers under.
	KnowledgeSearchToolName = "knowledge_search"
	// CodeSearchToolName is the tool name the code search answers under.
	CodeSearchToolName = "code_search"
)

// RecallToolName is the tool name the recall reference is assembled under.
const RecallToolName = "recall"

// ParamMinScore, ParamMaxResults, ParamSource and ParamTool are the
// configuration parameter names a preset reference reads.
const (
	// ParamTool selects the knowledge tool of a knowledge reference.
	ParamTool = "tool"
	// ParamSource names the local document sources of a knowledge reference.
	ParamSource = "source"
	// ParamMaxResults bounds the hits one knowledge search answers.
	ParamMaxResults = "max_results"
	// ParamMinScore drops the knowledge hits below the score.
	ParamMinScore = "min_score"
)

// ErrKnowledgeSourceMissing reports a knowledge reference that names no source.
var ErrKnowledgeSourceMissing = errors.New("knowledge: the reference names no local source")

// ErrKnowledgeEndpointUnwired reports a knowledge reference naming a remote endpoint.
var ErrKnowledgeEndpointUnwired = errors.New("knowledge: a remote endpoint is not wired")

// ErrKnowledgeToolUnknown reports a knowledge reference naming no known tool.
var ErrKnowledgeToolUnknown = errors.New("knowledge: the reference names no known tool")

// ErrPresetParam reports a preset parameter whose value cannot be read.
var ErrPresetParam = errors.New("preset: the parameter value cannot be read")

// ErrPresetNotWired reports a declared preset the assembly carries no face for.
var ErrPresetNotWired = errors.New("preset: the declared tool carries no wiring")

// RecallFunc is the memory retrieval face a recall tool answers through: the
// run's own recall over the committed store, never a retrieval of this package.
type RecallFunc func(ctx context.Context, q search.Query) (search.Result, error)

// PresetOptions fixes the faces a preset reference is assembled over.
type PresetOptions struct {
	// Recall is the run's own memory retrieval face; a recall reference
	// declared beside no face is refused by name.
	Recall RecallFunc
	// Subagent is the call_subagent tool wired by the composition root; a
	// call_subagent reference declared beside no tool is refused by name.
	Subagent *Tool
}

// ResolvePresets resolves every preset reference of one declaration into the
// tool it names, in declaration order. A reference naming no preset is left to
// the host-contributed tools and resolves to nothing here, so a declaration
// that references a contributed tool never fails on this step.
func ResolvePresets(ctx context.Context, refs []config.ToolRef, opts PresetOptions) ([]trpctool.Tool, error) {
	out := make([]trpctool.Tool, 0, len(refs))
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var (
			tool trpctool.Tool
			err  error
		)
		switch ref.Name {
		case KnowledgeRef:
			tool, err = newKnowledgeTool(ctx, ref)
		case RecallRef:
			if opts.Recall == nil {
				return nil, fmt.Errorf("%w: %s", ErrPresetNotWired, ref.Name)
			}
			tool = NewRecallTool(opts.Recall)
		case CallSubagentRef:
			if opts.Subagent == nil {
				return nil, fmt.Errorf("%w: %s", ErrPresetNotWired, ref.Name)
			}
			tool = opts.Subagent
		default:
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("preset %q: %w", ref.Name, err)
		}
		out = append(out, tool)
	}
	return out, nil
}

// newKnowledgeTool assembles one knowledge reference over the trpc knowledge
// stack: the sources the reference names are read into an in-memory vector
// store under the offline embedding binding, and the tool the reference selects
// answers from that store through the trpc knowledge tool.
func newKnowledgeTool(ctx context.Context, ref config.ToolRef) (trpctool.Tool, error) {
	if ref.Endpoint != "" {
		return nil, fmt.Errorf("%w: %s", ErrKnowledgeEndpointUnwired, ref.Endpoint)
	}
	paths := splitParam(ref.Params[ParamSource])
	if len(paths) == 0 {
		return nil, ErrKnowledgeSourceMissing
	}
	sources, err := knowledgeSources(paths)
	if err != nil {
		return nil, err
	}
	base := knowledge.New(
		knowledge.WithVectorStore(inmemory.New()),
		knowledge.WithEmbedder(knowledgeEmbedding{inner: memory.NewHashEmbedder(0)}),
		knowledge.WithSources(sources),
	)
	if err := base.Load(ctx); err != nil {
		return nil, fmt.Errorf("load the declared sources: %w", err)
	}
	options, err := knowledgeOptions(ref.Params)
	if err != nil {
		return nil, err
	}
	switch ref.Params[ParamTool] {
	case "", KnowledgeSearchToolName:
		return callableOf(knowledgetool.NewKnowledgeSearchTool(base, options...))
	case CodeSearchToolName:
		codeOptions := make([]knowledgetool.CodeSearchOption, 0, len(options))
		if name := ref.Params[ParamTool]; name != "" {
			codeOptions = append(codeOptions, knowledgetool.WithCodeSearchToolName(name))
		}
		if maxResults, ok := ref.Params[ParamMaxResults]; ok {
			n, err := strconv.Atoi(maxResults)
			if err != nil {
				return nil, fmt.Errorf("%w: %s=%q", ErrPresetParam, ParamMaxResults, maxResults)
			}
			codeOptions = append(codeOptions, knowledgetool.WithCodeSearchMaxResults(n))
		}
		if minScore, ok := ref.Params[ParamMinScore]; ok {
			score, err := strconv.ParseFloat(minScore, 64)
			if err != nil {
				return nil, fmt.Errorf("%w: %s=%q", ErrPresetParam, ParamMinScore, minScore)
			}
			codeOptions = append(codeOptions, knowledgetool.WithCodeSearchMinScore(score))
		}
		return callableOf(knowledgetool.NewCodeSearchTool(base, codeOptions...))
	default:
		return nil, fmt.Errorf("%w: %q", ErrKnowledgeToolUnknown, ref.Params[ParamTool])
	}
}

// knowledgeOptions reads the search options one knowledge reference declares.
func knowledgeOptions(params map[string]string) ([]knowledgetool.Option, error) {
	options := make([]knowledgetool.Option, 0, 3)
	if name := params[ParamTool]; name != "" {
		options = append(options, knowledgetool.WithToolName(name))
	}
	if maxResults, ok := params[ParamMaxResults]; ok {
		n, err := strconv.Atoi(maxResults)
		if err != nil {
			return nil, fmt.Errorf("%w: %s=%q", ErrPresetParam, ParamMaxResults, maxResults)
		}
		options = append(options, knowledgetool.WithMaxResults(n))
	}
	if minScore, ok := params[ParamMinScore]; ok {
		score, err := strconv.ParseFloat(minScore, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: %s=%q", ErrPresetParam, ParamMinScore, minScore)
		}
		options = append(options, knowledgetool.WithMinScore(score))
	}
	return options, nil
}

// knowledgeSources reads every declared path into the source that serves it: a
// directory is read recursively and a regular file is read as one document.
func knowledgeSources(paths []string) ([]source.Source, error) {
	sources := make([]source.Source, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("knowledge: source %q: %w", path, err)
		}
		if info.IsDir() {
			sources = append(sources, dirsource.New([]string{path}, dirsource.WithRecursive(true)))
			continue
		}
		sources = append(sources, filesource.New([]string{path}))
	}
	return sources, nil
}

// callableOf adapts one trpc knowledge tool onto the model-visible call
// surface: the declaration and the search stay the trpc tool's own, and a call
// answers with the JSON encoding of the response it produced, so the document
// text reaches the model instead of the address of a response value. A tool
// carrying no native call surface is refused by name.
func callableOf(tool trpctool.Tool) (trpctool.Tool, error) {
	callable, ok := tool.(trpctool.CallableTool)
	if !ok || callable == nil {
		return nil, fmt.Errorf("%w: the trpc knowledge tool carries no native call surface", ErrKnowledgeToolUnknown)
	}
	return &knowledgeTool{inner: callable}, nil
}

// knowledgeTool is the model-visible face of one trpc knowledge tool: the
// declaration it reports is the trpc tool's own and its search is the trpc one,
// and only the answer is encoded as the text a tool result carries.
type knowledgeTool struct {
	inner trpctool.CallableTool
}

// The adapter satisfies the native callable tool port.
var _ trpctool.CallableTool = (*knowledgeTool)(nil)

// Declaration reports the declaration of the wrapped trpc knowledge tool.
func (t *knowledgeTool) Declaration() *trpctool.Declaration {
	return t.inner.Declaration()
}

// Call runs one search through the wrapped trpc knowledge tool and answers
// with its response encoded as text.
func (t *knowledgeTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	out, err := t.inner.Call(ctx, jsonArgs)
	if err != nil {
		return nil, err
	}
	switch out.(type) {
	case nil, string:
		return out, nil
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("knowledge: encode the answer: %w", err)
	}
	return string(encoded), nil
}

// splitParam splits one comma-separated parameter into its trimmed entries.
func splitParam(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := make([]string, 0, 2)
	for _, entry := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// knowledgeEmbedding adapts the memory-side offline embedding binding onto the
// trpc knowledge embedder port: the vector is the deterministic one the memory
// side already owns, so a knowledge search runs offline and the retrieval
// stack stays the trpc one.
type knowledgeEmbedding struct {
	inner memory.Embedder
}

// The adapter satisfies the trpc knowledge embedder port.
var _ embedder.Embedder = knowledgeEmbedding{}

// GetEmbedding returns the vector of text under the binding.
func (e knowledgeEmbedding) GetEmbedding(ctx context.Context, text string) ([]float64, error) {
	vector, err := e.get(ctx, text)
	return vector, err
}

// GetEmbeddingWithUsage returns the vector of text and no usage record.
func (e knowledgeEmbedding) GetEmbeddingWithUsage(ctx context.Context, text string) ([]float64, map[string]any, error) {
	vector, err := e.get(ctx, text)
	return vector, nil, err
}

// GetDimensions reports the width of the vector the binding produces.
func (e knowledgeEmbedding) GetDimensions() int { return memory.DefaultHashDimensions }

// get returns the vector of text widened onto the trpc embedder width.
func (e knowledgeEmbedding) get(ctx context.Context, text string) ([]float64, error) {
	vector, err := e.inner.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	out := make([]float64, len(vector))
	for i, component := range vector {
		out[i] = float64(component)
	}
	return out, nil
}

// recallArgs is the declared argument shape of one recall call.
type recallArgs struct {
	// Text is the retrieval content of the query.
	Text string `json:"text"`
	// Strategy names the retrieval rule, one of the memory search strategies.
	Strategy string `json:"strategy"`
	// Cursor is the continuation token of the page this query asks for.
	Cursor string `json:"cursor"`
	// Partitions is the authorized partition set the query may read.
	Partitions []string `json:"partitions"`
	// Types restricts the query to the given frozen event types.
	Types []string `json:"types"`
	// Limit caps how many references one page carries.
	Limit int `json:"limit"`
}

// recallHit is one reference a recall call hit, in the reference shape the
// memory side answers with: the stored original is named, never copied.
type recallHit struct {
	// Key is the canonical identity of the stored original.
	Key event.EventID `json:"key"`
	// Owner is the address the reference belongs to.
	Owner event.Address `json:"owner"`
	// Format is the format the original was registered in.
	Format string `json:"format"`
	// RetentionPolicy is the retention policy the original is held under.
	RetentionPolicy string `json:"retention"`
}

// recallResponse is the answer of one recall call.
type recallResponse struct {
	// Refs is the reference set the query hit, best hit first.
	Refs []recallHit `json:"refs"`
	// NextCursor is the continuation token of the page after this one.
	NextCursor string `json:"next_cursor,omitempty"`
}

// NewRecallTool returns the recall tool over the memory retrieval face the
// caller injects: the tool holds no retrieval of its own, so its behaviour is
// exactly the face it was assembled over.
func NewRecallTool(recall RecallFunc) trpctool.Tool {
	return &recallTool{recall: recall}
}

// recallTool is the model-visible face of one memory retrieval.
type recallTool struct {
	recall RecallFunc
}

// The recall tool satisfies the native callable tool port.
var _ trpctool.CallableTool = (*recallTool)(nil)

// Declaration reports the recall tool's declaration.
func (t *recallTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name: RecallToolName,
		Description: "recall the stored events of this run by one retrieval query and answer with the references they hit: " +
			"a hit names the stored original and never copies its text, so the page is read through the reference afterwards. " +
			"the query runs only inside the partitions it names, and an empty partition set authorizes nothing.",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"text": {
					Type:        "string",
					Description: "the retrieval content of the query; a query without text is refused",
				},
				"strategy": {
					Type:        "string",
					Description: "the retrieval rule: hybrid, keyword_only, or empty for the port's own choice",
				},
				"cursor": {
					Type:        "string",
					Description: "the continuation token a previous page answered with",
				},
				"partitions": {
					Type:        "array",
					Items:       &trpctool.Schema{Type: "string"},
					Description: "the authorized partition set the query may read",
				},
				"types": {
					Type:        "array",
					Items:       &trpctool.Schema{Type: "string"},
					Description: "the frozen event types the query is restricted to",
				},
				"limit": {
					Type:        "integer",
					Description: "the page bound; a non-positive bound runs under the port's default",
				},
			},
			Required: []string{"text"},
		},
	}
}

// Call runs one retrieval through the injected face and answers with a JSON
// page: the references the query hit beside the cursor of the page after it.
func (t *recallTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args recallArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("%w: %v", search.ErrQueryTextMissing, err)
	}
	q := search.Query{
		Text:       args.Text,
		Strategy:   search.Strategy(args.Strategy),
		Cursor:     search.Cursor(args.Cursor),
		Partitions: args.Partitions,
		Types:      args.Types,
		Limit:      args.Limit,
	}
	if err := q.Validate(); err != nil {
		return nil, err
	}
	result, err := t.recall(ctx, q)
	if err != nil {
		return nil, err
	}
	out := recallResponse{NextCursor: string(result.NextCursor), Refs: make([]recallHit, 0, len(result.Refs))}
	for _, ref := range result.Refs {
		out.Refs = append(out.Refs, recallHit{
			Key:             ref.Key,
			Owner:           ref.Owner,
			Format:          ref.Format,
			RetentionPolicy: ref.RetentionPolicy,
		})
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("recall: encode the page: %w", err)
	}
	return string(encoded), nil
}
