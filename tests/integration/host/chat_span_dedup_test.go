// The chat-span dedup checks of the host: one model request is one generation.
// The generation span is the framework's — the chat span a model step of the
// framework flow opens — and the model-response face of the observation leaves
// that span alone, so a request lands one generation instead of one real span
// beside an empty second one.
package host

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/SpellingDragon/tagent/api/model"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	semconvtrace "trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/trace"
	ftrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

// generationSpanPrefix is the name prefix a model request's generation span
// carries on the framework tracer: the chat operation beside the model name.
const generationSpanPrefix = "chat"

// frameworkGenerationRecorder installs a real SDK tracer on the framework
// tracer the flow and the observation both read, and answers the spans it
// ended, so one check reads the span a model request really exports.
func frameworkGenerationRecorder(tb testing.TB) *tracetest.SpanRecorder {
	tb.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	originalTracer, originalProvider := ftrace.Tracer, ftrace.TracerProvider
	ftrace.Tracer, ftrace.TracerProvider = provider.Tracer("tagent-generation-check"), provider
	tb.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		ftrace.Tracer, ftrace.TracerProvider = originalTracer, originalProvider
	})
	return recorder
}

// watchModelResponseLogger points the framework logger at the given stand-in for
// the duration of one check, so the model-response line is read from its own
// sink.
func watchModelResponseLogger(tb testing.TB, logger trpclog.Logger) {
	tb.Helper()
	original := trpclog.Default
	trpclog.Default = logger
	tb.Cleanup(func() { trpclog.Default = original })
}

// driveFrameworkModelStep runs one model step of a framework llm agent over the
// named model, and answers every completion notice the flow asks for the way a
// session service does, so the step runs instead of waiting for a runner.
func driveFrameworkModelStep(tb testing.TB, ctx context.Context, agentName string, provider model.Model) {
	tb.Helper()
	drive := llmagent.New(agentName, llmagent.WithModel(provider))
	invocation := trpcagent.NewInvocation(
		trpcagent.WithInvocationID("inv-"+agentName),
		trpcagent.WithInvocationMessage(model.Message{Role: model.RoleUser, Content: "hello"}),
	)
	events, err := drive.Run(ctx, invocation)
	if err != nil {
		tb.Fatalf("run the framework model step: %v", err)
	}
	for evt := range events {
		if evt != nil && evt.RequiresCompletion {
			_ = invocation.NotifyCompletion(ctx, trpcagent.GetAppendEventNoticeKey(evt.ID))
		}
	}
}

// reduceModelRequest drives one model request through the runtime's own entry:
// the provider answers with its stream and the single terminal reduction reads
// it, exactly as the agent loop calls it.
func reduceModelRequest(tb testing.TB, ctx context.Context, provider model.Model) model.Outcome {
	tb.Helper()
	stream, err := provider.GenerateContent(ctx, &model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err != nil {
		tb.Fatalf("open the model stream: %v", err)
	}
	return model.ReduceChannel(ctx, stream, nil)
}

// generationSpans reports the ended spans of the generation family.
func generationSpans(recorder *tracetest.SpanRecorder) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if strings.HasPrefix(span.Name(), generationSpanPrefix) {
			out = append(out, span)
		}
	}
	return out
}

// spanAttribute reports the rendered value of one attribute of a span.
func spanAttribute(span sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// finalAnswerModel is the provider of one served request: it answers every
// request with the final turn the caller scripts, named by the model that
// served it as a provider reports it.
func finalAnswerModel(name, content string) *entryModel {
	return &entryModel{name: name, answer: func(*model.Request) *model.Response {
		answer := entryFinalTurn(content)
		answer.Model = name
		return answer
	}}
}

// TestModelRequestIsOneGeneration checks the generation span of one model request.
// - 框架侧: one framework model step opens exactly one generation span, named after the chat operation and the model
// - 去重: driving a model request through the runtime's reduction adds no second span beside the framework's
// - 日志面: the reduction still renders its one bounded INFO line, so the dedup removes the span and never the line
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestModelRequestIsOneGeneration(t *testing.T) {
	ctx := context.Background()

	t.Run("框架侧一次模型调用一条 generation", func(t *testing.T) {
		recorder := frameworkGenerationRecorder(t)
		driveFrameworkModelStep(t, ctx, "agent-framework", finalAnswerModel("model-a", "settled"))
		spans := generationSpans(recorder)
		if len(spans) != 1 {
			t.Fatalf("generation spans = %d, want the one chat span of the model request", len(spans))
		}
		if got, want := spans[0].Name(), "chat model-a"; got != want {
			t.Fatalf("generation span name = %q, want %q", got, want)
		}
		if got, ok := spanAttribute(spans[0], string(semconvtrace.KeyGenAIOperationName)); !ok || got.AsString() != "chat" {
			t.Fatalf("gen_ai.operation.name = %v (present %t), want chat", got.AsString(), ok)
		}
	})

	t.Run("本仓归约不在框架 span 旁添第二条", func(t *testing.T) {
		recorder := frameworkGenerationRecorder(t)
		driveFrameworkModelStep(t, ctx, "agent-framework", finalAnswerModel("model-a", "settled"))
		if got := len(generationSpans(recorder)); got != 1 {
			t.Fatalf("generation spans = %d after the framework step, want the one chat span", got)
		}

		outcome := reduceModelRequest(t, ctx, finalAnswerModel("model-a", "settled"))

		if outcome.Kind != model.KindCompleted {
			t.Fatalf("terminal = %s, want the completed terminal of the request", outcome.Kind)
		}
		if got := len(generationSpans(recorder)); got != 1 {
			t.Fatalf("generation spans = %d after the reduction, want the framework's one and no second", got)
		}
	})

	t.Run("去重不删 INFO 行", func(t *testing.T) {
		recorder := frameworkGenerationRecorder(t)
		logger := &observationLogger{}
		watchModelResponseLogger(t, logger)

		outcome := reduceModelRequest(t, ctx, finalAnswerModel("model-a", "settled"))

		if outcome.Kind != model.KindCompleted {
			t.Fatalf("terminal = %s, want the completed terminal of the request", outcome.Kind)
		}
		if got := len(generationSpans(recorder)); got != 0 {
			t.Fatalf("generation spans = %d, want the reduction to open none of its own", got)
		}
		if !logger.mentions("info", "model response", "model=model-a", "terminal=completed", "responses=1", "latency_ms=") {
			t.Fatalf("info lines = %v, want the bounded model-response line of the request", logger.at("info"))
		}
	})
}
