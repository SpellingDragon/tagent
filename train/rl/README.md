# tagent × AReaL Integration

Bridge tagent's persistent event loop with AReaL's RL training framework.

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│ AReaL (Python)                                               │
│  ┌─────────────┐    ┌──────────────┐    ┌─────────────────┐  │
│  │ PPOTrainer  │←──│ RolloutCtrl  │←──│ OpenAI Proxy    │  │
│  │ (actor/crit)│    │ (orchestr.)  │    │ (动态端口,       │  │
│  └─────────────┘    └──────┬───────┘    │  logprob capt.) │  │
│                            │            └────────┬────────┘  │
│                     ┌──────┴───────┐             │           │
│                     │ TagentAdapter│             │ proxy_addr│
│                     │ (Python)     │             │ (base_url)│
│                     └──────┬───────┘             │           │
└────────────────────────────┼─────────────────────┼───────────┘
                             │ HTTP                │
                             │ POST /task          │
                             │ {messages,          │
                             │  llm_base_url}      │
                             ▼                     ▼
┌──────────────────────────────────────────────────────────────┐
│ tagent (Go)                                                  │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────────┐  │
│  │ HTTP API     │   │ Persistent   │   │ SwappableModel   │  │
│  │ /task        │──→│ Event Loop   │──→│ .Swap(newModel   │  │
│  │ /healthz     │   │              │   │  with proxy URL) │  │
│  └──────┬───────┘   └──────────────┘   └────────┬─────────┘  │
│         │ modelUpdateFn                           │ LLM req   │
│         └─────────────────────────────────────────┘           │
└───────────────────────────────────────────────────────────────┘
                                         │
                                         ▼
                              AReaL Proxy (捕获 logprobs)
```

## Data Flow

1. AReaL starts OpenAI-compatible proxy (dynamically allocated port) + PPO trainer
2. AReaL's `OpenAIProxyWorkflow` passes proxy URL to adapter via `extra_kwargs["base_url"]`
3. `TagentARealAdapter.run()` sends `POST /task {messages, llm_base_url: proxy_addr}` to tagent
4. tagent's HTTPAPI calls `modelUpdateFn(proxy_addr)` → `SwappableModel.Swap(newModel)`
5. tagent's persistent loop processes the task:
   - All LLM requests go through the new model → AReaL proxy
   - AReaL's InteractionCache records logprobs + completion_ids at the proxy level
6. Adapter waits (`asyncio.sleep(wait_time)`) for tagent to process the task
7. Adapter returns episode-level reward (float) to AReaL
8. AReaL's PPO trainer uses (logprobs from proxy + reward from adapter) for policy update

**Note:** During online RL training, logprobs and reward data are handled by AReaL.
tagent can also independently record trajectories via `TrajectoryRecorder` (see Offline Data Pipeline below).

## RL Data Recording (Online Training)

During online RL training, AReaL handles all RL-specific data:

| Data | Recorder |
|------|----------|
| logprobs | AReaL proxy (SGLang) |
| completion_ids | AReaL InteractionCache |
| input_ids / output_ids | AReaL proxy (tokenizer) |
| reward | AReaL adapter (reward_fn) |
| conversation tree | AReaL InteractionCache |

For offline data collection (without AReaL), see [Offline Data Pipeline](#offline-data-pipeline) below.


## Offline Data Pipeline

tagent's `TrajectoryRecorder` records every LLM call to JSONL files during daily operation (even without AReaL).
These trajectories can be converted to AReaL-compatible datasets for offline training.

### Recording (daily operation)

```yaml
# tagent.yaml — enable trajectory recording
trajectory_dump: true
trajectory_dir: "data/trajectories"
```

Files: `data/trajectories/{session_id}.jsonl` (one JSON line per LLM call)

### Conversion

```bash
# SFT dataset: {input_ids, loss_mask} for AReaL SFTTrainer
python3 areal/convert_trajectories.py     --input data/trajectories/     --output data/sft/     --tokenizer Qwen/Qwen2.5-1.5B-Instruct     --mode sft

# RL prompt dataset: {messages} for AReaL PPOTrainer (online RL)
python3 areal/convert_trajectories.py     --input data/trajectories/     --output data/rl/     --mode rl
```

### Training paths

| Path | Dataset format | AReaL trainer | Requires logprobs? |
|------|---------------|---------------|-------------------|
| SFT | {input_ids, loss_mask} | SFTTrainer | No |
| Online RL | {messages} (prompt only) | PPOTrainer | Yes (captured by AReaL proxy during training) |

**Note**: Trajectories collected in normal mode (ZhipuAI) do not contain logprobs.
They are suitable for SFT or as prompt datasets for online RL (where AReaL generates fresh responses with logprobs).

## Setup

### 1. Start tagent with HTTP API

```go
package main

import (
    "net/http"

    "github.com/SpellingDragon/tagent/agent"
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
)

func main() {
    // Create initial model (endpoint is a placeholder — will be swapped
    // by AReaL's dynamic proxy URL via llm_base_url).
    llm := openai.New(
        "your-model",
        openai.WithBaseURL("http://localhost:8000/v1"), // placeholder
        openai.WithAPIKey("dummy"),
    )

    // Wrap in SwappableModel — allows runtime LLM endpoint updates
    // without recreating the agent or changing the event mechanism.
    swappableModel := agent.NewSwappableModel(llm)

    // Create tagent agent
    ta, _ := agent.NewTagentAgent(&agent.TagentConfig{
        Model: swappableModel,
    })

    // Start persistent loop
    outputCh, _ := ta.StartLoop("rl-user", "rl-session")

    // Start HTTP API with model update callback.
    // When AReaL adapter sends POST /task with llm_base_url,
    // this callback creates a new model pointing to AReaL's proxy
    // (dynamically allocated port) and swaps it in.
    api := agent.NewHTTPAPI(ta)
    api.SetModelUpdateFn(func(baseURL string) {
        newModel := openai.New(
            "your-model",
            openai.WithBaseURL(baseURL),
            openai.WithAPIKey("dummy"),
        )
        swappableModel.Swap(newModel)
    })
    http.ListenAndServe(":8089", api)

    _ = outputCh
}
```

### 2. Configure AReaL

```yaml
# areal_config.yaml
rollout:
  openai:
    mode: inline
    workflow: areal.tagent_adapter.TagentARealAdapter

# Environment variables for the adapter
# TAGENT_URL=http://localhost:8089
# TAGENT_USER_ID=rl-user
# TAGENT_SESSION_ID=rl-session
```

### 3. Run training

```bash
# Terminal 1: Start tagent in RL mode (auto-sets tagent.rl.yaml + RL session params)
export AREAL_API_KEY=your-key
./run.sh rl

# Terminal 2: Start AReaL training
./run.sh areal

# Local debug (single GPU, no torchrun):
AREAL_USE_TORCHRUN=false AREAL_N_GPUS=1 \
AREAL_EXTRA_ARGS="scheduler.type=local" ./run.sh areal
```

## Reward Functions

The adapter supports optional Python-side reward functions:

```python
from areal.tagent_adapter import TagentARealAdapter

def my_reward_fn(data: dict) -> float:
    """Custom reward based on task data."""
    messages = data.get("messages", [])
    # Custom logic: check task content, expected outcomes, etc.
    if any("correct" in m.get("content", "") for m in messages):
        return 1.0
    return 0.0

adapter = TagentARealAdapter(
    tagent_url="http://localhost:8089",
    reward_fn=my_reward_fn,
    wait_time=60.0,  # seconds to wait for tagent processing
)
```

If no reward function is provided, the adapter returns 0.0 and AReaL's
training loop handles reward assignment internally.

## HTTP API Endpoints

| Method | Path | Body Fields | Description |
|--------|------|-------------|-------------|
| POST | `/task` | `messages`, `user_id?`, `session_id?`, `llm_base_url?` | Submit task to persistent loop. `llm_base_url` triggers `SwappableModel.Swap` to redirect LLM requests to AReaL proxy. |
| GET | `/healthz` | — | Health check (includes `loop_active` status) |

## Key Design Points

- **Dynamic proxy port**: AReaL proxy port is dynamically allocated (`find_free_ports()`), not fixed at 8000. The actual URL is passed via `extra_kwargs["base_url"]` → `POST /task llm_base_url` → `SwappableModel.Swap`.
- **Logprobs**: Captured by AReaL's proxy at the LLM layer. tagent does NOT capture logprobs.
- **Completion IDs**: Captured by AReaL's InteractionCache at the proxy level. Used to map rewards to interactions.
- **Trajectory recording (optional)**: tagent can record LLM call trajectories via `TrajectoryRecorder` (`trajectory_dump: true`). See Offline Data Pipeline above.
- **Reward computation**: During online RL, reward is computed by the adapter or AReaL's training loop, not tagent.
- **Event mechanism unchanged**: `SwappableModel` only wraps `model.Model` — persistent loop, `InjectMessage`, `outputCh`, and `POST /task` async 202 semantics are all unchanged.
- **Tool output interception**: tagent wraps all tools with `OutputLimitTool` to prevent oversized tool outputs from consuming the context window. The limit is `MaxTokens / 2 * 4` characters.
