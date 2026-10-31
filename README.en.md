# tagent — Agents that keep working

**An event-driven Go AI agent framework for building Super-agent systems: main–subagent orchestration, retrievable memory, composable plugins and recoverable work.**

[简体中文](README.md) | English

Build an agent into your own application. Give it inputs, tools, independent workers, memory and a delivery interface. Keep the evidence needed to understand what happened and continue unfinished responsibilities after a process restart. tagent is built on `trpc-agent-go`; the CLI and WeChat bot are example hosts.

## Start without credentials

Go 1.24.1+:

```bash
git clone https://github.com/SpellingDragon/tagent.git
cd tagent/examples/cli-host
go run . -c "hello"
# mock reply: hello
```

This exercises the real public assembly and invocation path with a local mock model. See the [CLI guide](examples/cli-host/README.md) for a real provider, interactive input and optional storage.

## What could you build?

- **Engineering collaborators:** delegate investigation, review and execution; connect your issue, PR and CI feeds.
- **Personal and team assistants:** follow ongoing work through the channels you supply.
- **Research workflows:** separate retrieval, analysis and verification; revisit original evidence on demand.
- **Operations agents:** launch external jobs, observe results and recover unfinished responsibilities.
- **Agent evaluation and training infrastructure:** capture actual model requests, tool outcomes and exportable trajectories.

These are framework composition directions. External connectors are not all bundled. Embed the Go runtime, write a host, or start with the [CLI](examples/cli-host/) and [WeChat](examples/wechat-bot/) examples.

## A practical foundation for Super-agents

| Design idea | Runnable building blocks | Scope |
|---|---|---|
| Continuity | Explicit session ownership, delegation, durable facts, context projection and recall | Hosts/plugins supply cross-channel and thread associations |
| Efficient work | Background task admission, child execution and terminal-result backflow | Full-duplex same-session intake and cross-source batching remain under convergence in next |
| Proactive behavior | External inputs, model decisions, tools and reflection inputs | Applications supply sources, rules and authorization; the measured next reflection smoke failed |
| Persistence | Local recovery, execution-resource observation, durable delivery obligations and receipts | Primarily a trusted, single-machine scope; semantic supervision and distributed deployment require additional work |

**One useful organization is main–subagent orchestration.** The main agent coordinates interactions; child agents execute in separate contexts. Instructions, visible tools and input ownership make that division concrete. Direct topic-to-child-session routing is a host/plugin composition point, not a claim that a complete subscription system ships by default. Public discussions of Super-agents are linked under [References and acknowledgments](#references-and-acknowledgments).

## Event-driven and composable

Inputs and execution outcomes become traceable facts. Plugins govern calls, contribute environment information, observe facts, shape model-visible views and request compression. Projections and request assembly determine what the model sees; retained originals support later reads.

The six named ports and dependency boundaries are documented in [extensions](docs/EXTENSIONS.md). The public entry points live in `assembly/`; optional governance, reflection, telemetry and training capabilities live under `features/`.

## The hard parts of running continuously

- **Accepted is not completed:** terminal outcomes must reach the right session and be consumed.
- **Background execution can still block intake:** dispatch-wait and per-invocation drives motivate the next consumer-loop convergence.
- **Memory must remain inspectable:** age-based folding, budgeted views and segmented original reads preserve a route back to evidence.
- **Generated is not delivered:** persist an obligation before sending, clear it after the receipt, and recover outstanding deliveries. A send-before-receipt crash can produce a duplicate.
- **Configuration changes must not rewrite running work:** borrowed execution snapshots retain their initiating binding.
- **Event floods and reflection loops need discipline:** admission rules, observation scopes, cadence and governance must be checked against running behavior.
- **A dead process cannot restart itself:** external supervision handles infrastructure recovery; semantic task-progress supervision is a separate responsibility.

Captured trajectories support evaluation and training-data export. They do not imply automatic online model-weight training.

## Measured smoke-host throughput

Measured on **2026-11-01**, development snapshot [`21ba86a`](https://github.com/SpellingDragon/tagent/tree/21ba86a371d1c932aec4d49ee49ee9dadaf832a7), Go 1.24.1, linux/amd64, GOMAXPROCS=2. Each case uses 30 operations, repeated three times; values below use median ns/op.

| Scripted-model scenario | Median time/op | Derived throughput |
|---|---:|---:|
| Synchronous answer and delivery | 4.091 ms/op | 244.4 rounds/s |
| Delegation through terminal-result delivery | 27.053 ms/op | 37.0 operations/s |
| Folding off | 4.489 ms/op | 222.8 rounds/s |
| Folding on | 3.218 ms/op | 310.8 rounds/s |
| Three concurrent sessions | 2.527 ms/op | 395.8 rounds/s |
| Ten concurrent sessions | 3.857 ms/op | 259.3 rounds/s |

This measures one production-host assembly with a scripted local HTTP model and recording output channel. It is not real LLM throughput, live WeChat performance, or a capacity guarantee for every host. The small samples do not establish a compression speedup or linear concurrency scaling.

**Validation is mixed and reported as such.** Build, CLI mock and all 18 benchmark samples passed. The long smoke completed assertions over 300 pump rounds, 45 delegations, 15 approvals, file/long outputs and history folding, then failed waiting for reflection for two minutes. The full test is **FAIL**. In the inspected provider nightly, both real LLM and embedding jobs were **SKIPPED**.

[Commands, raw measurements, failure details and scope →](docs/SMOKE-RESULTS.md)

## Explore and contribute

[Project overview](docs/PROJECT.md) · [Mechanism tour](docs/wiki/mechanism-tour.md) · [Extensions](docs/EXTENSIONS.md) · [API](docs/api/index.md) · [Operations](docs/OPERATION.md) · [Contributing](CONTRIBUTING.md) · [Changelog](CHANGELOG.md)

Module map: `assembly/` composition and hosts; `api/` contracts; `runtime/` agents and task responsibility; `memory/` facts and projections; `tool/` adapters; `features/` optional capabilities. Both example hosts are separate Go modules.

Pre-release: APIs, configuration and storage formats may change. Current deployments primarily assume a trusted single machine. Local atomic replacement is not a power-loss durability guarantee. See `go.mod` for the upstream fork boundary.

Contributions welcome: new hosts, event sources, tools, recovery scenarios and reproducible evaluations.

## References and acknowledgments

- Thank you to the maintainers and contributors of [tRPC-Agent-Go](https://github.com/trpc-group/trpc-agent-go) for the underlying framework and tool ecosystem used by tagent.
- Thank you to the DeepSeek Harness team for publishing the [Super-agent design article](https://mp.weixin.qq.com/s/zoS7GlReWPhfCjENGxXCdg), [DSH architecture documentation](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md) and [dsh-superagent-prototype](https://github.com/deepseek-ai/dsh-superagent-prototype). They offer valuable public discussions and examples of long-running agents, events and plugin composition.
- Related exploration: [SpellingDragon/dsh-superagent-prototype](https://github.com/SpellingDragon/dsh-superagent-prototype) is an experimental fork of the upstream prototype. Its `tagent` branch explores call-boundary constraints, recoverable delivery obligations and progressive policy (record → warn → enforce). Execution remains on DSH; the fork does not import tagent's runtime.

tagent is maintained independently. These materials are cited for technical reading and do not imply affiliation, collaboration or endorsement by DeepSeek. Actual dependencies are listed in [go.mod](go.mod).

[Apache-2.0](LICENSE)
