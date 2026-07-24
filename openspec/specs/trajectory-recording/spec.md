# trajectory-recording Specification

## Purpose

本规范定义 trajectory-recording 能力。The system SHALL provide a `TrajectoryRecorder` type that implements the `model.Model` interface and wraps an inner `model.Model`.

## Requirements

### Requirement: TrajectoryRecorder wraps model.Model

The system SHALL provide a `TrajectoryRecorder` type that implements the `model.Model` interface and wraps an inner `model.Model`. All LLM requests forwarded through TrajectoryRecorder SHALL be recorded before being passed to the inner model. The inner model's response SHALL be recorded before being returned to the caller.

#### Scenario: Normal mode recording

- **WHEN** tagent runs in normal mode (ZhipuAI) with `trajectory_dump: true`
- **AND** TrajectoryRecorder wraps the ZhipuAI model
- **THEN** every LLM call's request messages, response content, tool calls, usage, and timing SHALL be recorded to a JSONL file

#### Scenario: RL mode recording

- **WHEN** tagent runs in RL mode (AReaL proxy) with `trajectory_dump: true`
- **AND** TrajectoryRecorder wraps SwappableModel
- **THEN** every LLM call through the AReaL proxy SHALL be recorded to a JSONL file
- **AND** the AReaL proxy's own InteractionCache recording SHALL continue to function independently

#### Scenario: Recording disabled

- **WHEN** `trajectory_dump` is `false` or unset
- **THEN** TrajectoryRecorder SHALL NOT be instantiated
- **AND** no trajectory files SHALL be written

### Requirement: JSONL trajectory format

The system SHALL persist trajectory records as JSONL files at `{trajectory_dir}/{session_id}.jsonl`. Each line SHALL be a JSON object containing the following fields: `timestamp`, `session_id`, `user_id`, `batch_index`, `llm_call.request.messages`, `llm_call.request.model`, `llm_call.request.generation_config`, `llm_call.response.choices`, `llm_call.response.usage`, `llm_call.response.finish_reason`, `llm_call.response.error`, `metadata.duration_ms`, `metadata.model_endpoint`.

#### Scenario: Single LLM call produces one JSONL line

- **WHEN** an LLM call completes (either success or error)
- **THEN** exactly one JSON line SHALL be appended to the session's JSONL file
- **AND** the line SHALL contain the full request messages and response choices

#### Scenario: Tool calls are recorded

- **WHEN** the LLM response contains tool calls
- **THEN** the `llm_call.response.choices` field SHALL include the complete tool call definitions (name, arguments, id)

### Requirement: Async disk writing

The system SHALL write trajectory records to disk asynchronously using a buffered channel and a background goroutine. The LLM call path SHALL NOT block on disk I/O.

#### Scenario: Channel full does not block LLM calls

- **WHEN** the internal buffered channel is full
- **AND** a new trajectory record needs to be recorded
- **THEN** the record SHALL be dropped
- **AND** a warning log SHALL be emitted
- **AND** the LLM call SHALL continue without delay

#### Scenario: Graceful shutdown flushes pending records

- **WHEN** TrajectoryRecorder's `Close()` method is called
- **THEN** all pending records in the channel SHALL be flushed to disk before returning
- **AND** the JSONL file SHALL be properly closed

### Requirement: Configuration via tagent.yaml

The system SHALL read `trajectory_dump` (bool, default `false`) and `trajectory_dir` (string, default `data/trajectories`) from the tagent config. When `trajectory_dump` is `true`, `tagent.New()` SHALL wrap the model with TrajectoryRecorder before passing it to agent creation.

#### Scenario: Config enables trajectory recording

- **WHEN** `tagent.yaml` contains `trajectory_dump: true` and `trajectory_dir: "data/trajectories"`
- **THEN** tagent.New() SHALL create a TrajectoryRecorder wrapping the configured model
- **AND** the trajectory directory SHALL be created if it does not exist

#### Scenario: Default config does not record

- **WHEN** `tagent.yaml` does not contain `trajectory_dump` or sets it to `false`
- **THEN** no TrajectoryRecorder SHALL be created
- **AND** the model SHALL be used directly without wrapping

### Requirement: Composability with SwappableModel

TrajectoryRecorder SHALL be composable with SwappableModel in either order: `TrajectoryRecorder(SwappableModel(model))` or `SwappableModel(TrajectoryRecorder(model))`. In RL mode, the recommended order is `TrajectoryRecorder(SwappableModel(model))` so that trajectory recording captures the final routed LLM calls.

#### Scenario: TrajectoryRecorder wraps SwappableModel

- **WHEN** TrajectoryRecorder wraps SwappableModel
- **AND** SwappableModel.Swap() replaces the inner model
- **THEN** subsequent LLM calls through TrajectoryRecorder SHALL be recorded with the new model's endpoint
- **AND** the `metadata.model_endpoint` field SHALL reflect the swapped model's endpoint

### Requirement: Conversion to AReaL SFT dataset

The system SHALL provide a `convert_trajectories.py` script that reads tagent JSONL files and produces a HuggingFace Dataset compatible with AReaL's `SFTTrainer`. The SFT dataset SHALL contain `input_ids` and `loss_mask` fields, where prompt tokens have `loss_mask=0` and completion tokens have `loss_mask=1`.

#### Scenario: Convert JSONL to SFT dataset

- **WHEN** `convert_trajectories.py` is run with `--input data/trajectories/ --output data/sft/ --tokenizer Qwen/Qwen2.5-1.5B-Instruct --mode sft`
- **THEN** a HuggingFace Dataset SHALL be created at the output path
- **AND** each sample SHALL have `input_ids` (tokenized prompt + completion) and `loss_mask` (0 for prompt, 1 for completion)

#### Scenario: Convert JSONL to RL prompt dataset

- **WHEN** `convert_trajectories.py` is run with `--mode rl`
- **THEN** each sample SHALL contain only the `messages` field (the initial user message)
- **AND** the dataset SHALL be compatible with AReaL's PPOTrainer prompt dataset format
