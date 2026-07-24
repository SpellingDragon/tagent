# view-transform-stateless Specification

## Purpose

本规范定义 view-transform-stateless 能力。When `oldSegments` is empty (all segments moved to recentSegments or total segment count is too small), SmartCompressor SHALL return the original messages witho

## Requirements

### Requirement: SmartCompressor returns original messages when no segments to compress

When `oldSegments` is empty (all segments moved to recentSegments or total segment count is too small), SmartCompressor SHALL return the original messages without adding any `[context_compress]` system message. This prevents token waste from empty compress notifications that confuse the LLM.

#### Scenario: oldSegments empty after split

- **WHEN** SmartCompressor splits messages into oldSegments and recentSegments
- **AND** oldSegments is empty (length 0)
- **THEN** SmartCompressor SHALL return the original messages unchanged
- **AND** no `[context_compress]` system message SHALL be added

### Requirement: protectPendingAsyncSegments removed from Compress

The `protectPendingAsyncSegments` function SHALL be removed from the Compress method. This function was based on the incorrect assumption that `{status:running}` in tool results indicates a pending async operation. In reality, ActionTool's TmuxExecResponse always returns `status:running`, causing nearly all segments with tool results to be "protected" and preventing SmartCompressor from discarding any old segments.

#### Scenario: Segments with running status are not protected

- **WHEN** oldSegments contains a segment with a RoleTool message containing `{"status":"running"}`
- **THEN** the segment SHALL NOT be moved to recentSegments
- **AND** the segment SHALL be discarded as part of normal compression

#### Scenario: SmartCompressor discards old segments normally

- **WHEN** token count exceeds threshold and oldSegments is non-empty
- **THEN** oldSegments SHALL be discarded
- **AND** recentSegments SHALL be retained
- **AND** a `[context_compress]` message with key+type+summary list SHALL be generated
