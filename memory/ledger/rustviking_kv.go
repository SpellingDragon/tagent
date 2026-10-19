package ledger

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

// ErrRustViking reports a rustviking CLI call that did not succeed, carrying the
// report the CLI made of it.
var ErrRustViking = errors.New("memory: rustviking CLI call failed")

// cliEnvelope is the one response shape every rustviking CLI call returns.
type cliEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

// cliEntry is the key-value entry a rustviking get or scan returns. A key the
// store does not hold comes back with a null value, so the client reports it as
// an absent key rather than as a call failure.
type cliEntry struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

// cliScanResult is the response of a rustviking prefix scan.
type cliScanResult struct {
	Prefix  string     `json:"prefix"`
	Count   int        `json:"count"`
	Entries []cliEntry `json:"entries"`
}

// rustvikingKV is the file tier key-value view: every operation drives the
// rustviking CLI and reads its JSON envelope, values cross the process boundary
// base64 encoded, and a successful call means the rustviking store already holds
// what the call carried. Scans ask for a bounded page in key order.
//
// 契约: docs/wiki/memory/memory-architecture.md#rv-cli
type rustvikingKV struct {
	binary string
	config string
}

// newRustVikingKV returns the key-value view over one rustviking executable and
// the config file passed to it.
func newRustVikingKV(binary, config string) *rustvikingKV {
	return &rustvikingKV{binary: binary, config: config}
}

var _ kvPort = (*rustvikingKV)(nil)

// kvRead returns the pairs the fetch names: one `kv get` for the key it names,
// or one bounded `kv scan` page in key order for the prefix it names.
func (r *rustvikingKV) kvRead(ctx context.Context, fetch kvFetch) ([]kvPair, error) {
	if fetch.key != "" {
		data, err := r.call(ctx, "kv", "get", "-k", fetch.key)
		if err != nil {
			return nil, err
		}
		var entry cliEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil, fmt.Errorf("%w: get response for key %q: %w", ErrRustViking, fetch.key, err)
		}
		if entry.Value == nil {
			return nil, nil
		}
		value, err := base64.StdEncoding.DecodeString(*entry.Value)
		if err != nil {
			return nil, fmt.Errorf("%w: value of key %q is not valid base64: %w", ErrRustViking, fetch.key, err)
		}
		return []kvPair{{key: fetch.key, value: value}}, nil
	}
	data, err := r.call(ctx, "kv", "scan", "-p", fetch.prefix, "-l", "1000000")
	if err != nil {
		return nil, err
	}
	var scan cliScanResult
	if err := json.Unmarshal(data, &scan); err != nil {
		return nil, fmt.Errorf("%w: scan response for prefix %q: %w", ErrRustViking, fetch.prefix, err)
	}
	out := make([]kvPair, 0, len(scan.Entries))
	for _, entry := range scan.Entries {
		var value []byte
		if entry.Value != nil {
			decoded, err := base64.StdEncoding.DecodeString(*entry.Value)
			if err != nil {
				return nil, fmt.Errorf("%w: value of key %q is not valid base64: %w", ErrRustViking, entry.Key, err)
			}
			value = decoded
		}
		out = append(out, kvPair{key: entry.Key, value: value})
	}
	return out, nil
}

// kvCommit applies the batch one CLI call per key and completes the barrier:
// every call this view returns success for has already been applied by the
// rustviking store, so no staged state outlives a commit. Deletes run before
// puts, and a key stands on one side or the other, so the order names no
// different state.
func (r *rustvikingKV) kvCommit(ctx context.Context, batch kvBatch) error {
	for _, key := range batch.deletes {
		if _, err := r.call(ctx, "kv", "del", "-k", key); err != nil {
			return err
		}
	}
	for _, put := range batch.puts {
		if _, err := r.call(ctx, "kv", "put", "-k", put.key, "-v", base64.StdEncoding.EncodeToString(put.value)); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// call runs one rustviking CLI operation and returns the data of its response
// envelope, reporting the CLI error text as the cause whenever the call did not
// succeed.
func (r *rustvikingKV) call(ctx context.Context, args ...string) (json.RawMessage, error) {
	full := make([]string, 0, len(args)+3)
	full = append(full, "-c", r.config)
	full = append(full, args...)
	output, err := exec.CommandContext(ctx, r.binary, full...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("%w: %s", ErrRustViking, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("%w: %s: %w", ErrRustViking, r.binary, err)
	}
	var envelope cliEnvelope
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, fmt.Errorf("%w: response is not the CLI envelope: %w", ErrRustViking, err)
	}
	if !envelope.Success {
		return nil, fmt.Errorf("%w: %s", ErrRustViking, envelope.Error)
	}
	return envelope.Data, nil
}
