# PRD: Code Node V2 (Function Reference Execution)

## 1. Document Info

- Name: Code Node V2
- Version: v1.0
- Status: Draft
- Scope: FlowWeave `NodeTypeFunc` only
- Out of Scope: `Iteration` changes, split function implementation, map/reduce orchestration

## 2. Background

Current `Code` node uses a placeholder executor and cannot be used as a production-grade execution unit.
To make workflows stable and maintainable, `Code` node must execute registered local functions with strict DSL input/output contracts.

## 3. Goals

- Upgrade `Code` node from script-based placeholder to function-reference execution.
- Enforce explicit `inputs` and `outputs` schema in DSL (name + type + required).
- Add deterministic runtime validation and standardized error codes.
- Keep integration with existing workflow engine and node execution events.

## 4. Non-Goals

- Do not support dynamic script text execution.
- Do not add remote function execution.
- Do not change `Iteration` behavior in this phase.
- Do not implement split-specific logic in this phase.

## 5. Users and Core Scenarios

- Workflow author defines a `Code` node that calls a local function by `function_ref`.
- Workflow runtime maps values from variable pool into typed function inputs.
- Runtime validates output schema before writing node outputs.

## 6. Functional Requirements

### FR-001 Node Config

`Code` node must contain:

- `function_ref` (required)
- `inputs` (required, non-empty)
- `outputs` (required, non-empty)

Optional:

- `timeout_ms`
- `strict_schema` (default: `true`)

### FR-002 Input Assembly and Validation

- Input values are assembled from `value_selector` or `default`.
- `required=true` without value causes failure.
- Type validation must run before function execution.

### FR-003 Function Execution

- Runtime resolves function by `function_ref` from local registry.
- Missing function causes failure with dedicated error code.
- Execution runs under timeout guard.

### FR-004 Output Validation

- Function output must be a JSON-like object (`map[string]interface{}` semantics).
- All required output fields must exist.
- All declared outputs must match declared type.
- If `strict_schema=true`, undeclared output fields are rejected.

### FR-005 Node Result

- On success: validated outputs are emitted as node outputs.
- On failure: node fails with standardized error code and message.
- Metadata must include function reference and elapsed time.

## 7. DSL Specification

### 7.1 Node Example

```json
{
  "type": "func",
  "title": "Normalize Payload",
  "function_ref": "data.normalize.v1",
  "timeout_ms": 3000,
  "strict_schema": true,
  "inputs": [
    {
      "name": "raw_text",
      "type": "string",
      "required": true,
      "value_selector": ["start_1", "query"]
    },
    {
      "name": "locale",
      "type": "string",
      "required": false,
      "default": "zh-CN"
    }
  ],
  "outputs": [
    { "name": "normalized_text", "type": "string", "required": true },
    { "name": "token_count", "type": "number", "required": true }
  ]
}
```

### 7.2 Input Field Definition

- `name`: string, unique within node
- `type`: string enum
- `required`: boolean
- `value_selector`: `[node_id, variable_name]` (optional when default exists)
- `default`: optional

### 7.3 Output Field Definition

- `name`: string, unique within node
- `type`: string enum
- `required`: boolean

### 7.4 Supported Types (Phase 1)

- `string`
- `number`
- `boolean`
- `object`
- `array<string>`
- `array<number>`
- `array<boolean>`
- `array<object>`

## 8. Runtime Design

### 8.1 Function Registry (Conceptual)

```go
type LocalFunction interface {
    Name() string
    Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)
}
```

Registry capabilities:

- Register function by unique name
- Resolve function by `function_ref`
- Reject duplicate registrations by default

### 8.2 Code Node Execution Pipeline

1. Parse and validate node config.
2. Build input map from variable pool selectors/defaults.
3. Validate input types.
4. Resolve and execute local function with timeout.
5. Validate output schema.
6. Emit validated outputs and metadata.

### 8.3 Metadata Contract

Node execution metadata should include:

- `function_ref`
- `timeout_ms`
- `strict_schema`
- `elapsed_ms`
- `input_validation`: `passed|failed`
- `output_validation`: `passed|failed`

## 9. Error Codes

- `CODE_NODE_INVALID_CONFIG`
- `CODE_NODE_FUNCTION_NOT_FOUND`
- `CODE_NODE_INPUT_MISSING`
- `CODE_NODE_INPUT_TYPE_MISMATCH`
- `CODE_NODE_EXEC_TIMEOUT`
- `CODE_NODE_EXEC_FAILED`
- `CODE_NODE_OUTPUT_MISSING`
- `CODE_NODE_OUTPUT_TYPE_MISMATCH`
- `CODE_NODE_OUTPUT_SCHEMA_VIOLATION`

## 10. API/Engine Integration Notes

- Keep `NodeTypeFunc` as the only function execution node type.
- Keep current graph engine contract unchanged.
- Keep event model unchanged (`node_run_started`, `node_run_succeeded`, `node_run_failed`).
- Failure behavior follows existing node error strategy handling in engine.

## 11. Compatibility and Release Policy

This change is intentionally breaking:

- Old script fields (`code`, `language`, `code_language`) are not executable paths in V2.
- Workflows must migrate to `function_ref + inputs + outputs` before rollout.

## 12. Acceptance Criteria

- AC-001 Valid config and function returns expected outputs.
- AC-002 Missing `function_ref` fails config validation.
- AC-003 Missing required input returns `CODE_NODE_INPUT_MISSING`.
- AC-004 Input type mismatch returns `CODE_NODE_INPUT_TYPE_MISMATCH`.
- AC-005 Function not found returns `CODE_NODE_FUNCTION_NOT_FOUND`.
- AC-006 Timeout returns `CODE_NODE_EXEC_TIMEOUT`.
- AC-007 Missing required output returns `CODE_NODE_OUTPUT_MISSING`.
- AC-008 Output type mismatch returns `CODE_NODE_OUTPUT_TYPE_MISMATCH`.
- AC-009 `strict_schema=true` rejects extra output fields.
- AC-010 Node metadata contains function execution context.

## 13. Test Plan

### 13.1 Unit Tests

- Config validation tests
- Input type validator tests
- Output schema validator tests
- Error code mapping tests
- Timeout behavior tests

### 13.2 Integration Tests

- `start -> code -> end` success case
- Function not found case
- Input missing/type mismatch cases
- Output missing/type mismatch cases
- Strict schema violation case

### 13.3 Regression Tests

- Existing workflow engine event flow remains unchanged
- Non-`Code` node workflows continue to pass

## 14. Risks and Mitigations

- Risk: Existing workflows break after rollout.
- Mitigation: Pre-release DSL scan and migration checklist.

- Risk: Function sprawl with unclear ownership.
- Mitigation: Naming convention + owner registry + code review gates.

- Risk: Runtime failures due to weak schema discipline.
- Mitigation: Enforce strict schema by default and add negative tests.

## 15. Milestones

- M1: DSL schema and function registry contract finalized
- M2: Code node runtime refactor completed
- M3: Error handling and metadata completed
- M4: Test suite green and documentation released
