# TDD: Code Node V2 (Function Reference Execution)

## 1. Scope

This TDD is the implementation design for [PRD_CODE_NODE_V2.md](./PRD_CODE_NODE_V2.md).

In scope:

- Refactor `NodeTypeFunc` to execute local registered functions (`function_ref`)
- Enforce typed `inputs` and `outputs` contract from DSL
- Standardize validation, error code mapping, timeout handling, and metadata

Out of scope:

- `Iteration` refactor
- split function business implementation
- script runtime compatibility

## 2. Architecture Overview

`Code` node V2 introduces three layers:

1. Config + schema layer
- Parse and validate DSL fields
- Define input/output schema models

2. Function registry layer
- Register local callable functions
- Resolve by `function_ref`

3. Runtime execution layer
- Assemble inputs from variable pool
- Validate input types
- Execute function with timeout context
- Validate output schema
- Emit outputs and metadata

## 3. Data Model

## 3.1 Node DSL Model (V2)

```go
type CodeNodeData struct {
    Type         string              `json:"type"`
    Title        string              `json:"title"`
    FunctionRef  string              `json:"function_ref"`
    TimeoutMS    int                 `json:"timeout_ms,omitempty"`
    StrictSchema *bool               `json:"strict_schema,omitempty"`
    Inputs       []CodeInputBinding  `json:"inputs"`
    Outputs      []CodeOutputBinding `json:"outputs"`
}

type CodeInputBinding struct {
    Name         string                 `json:"name"`
    Type         string                 `json:"type"`
    Required     bool                   `json:"required"`
    ValueSelector types.VariableSelector `json:"value_selector,omitempty"`
    Default      interface{}            `json:"default,omitempty"`
}

type CodeOutputBinding struct {
    Name     string `json:"name"`
    Type     string `json:"type"`
    Required bool   `json:"required"`
}
```

Behavioral defaults:

- `strict_schema`: default `true`
- `timeout_ms`: default `3000` (configurable constant)

## 3.2 Function Interface and Registry

```go
type LocalFunction interface {
    Name() string
    Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)
}
```

Thread-safe global registry:

- `RegisterFunction(fn LocalFunction) error`
- `GetFunction(name string) (LocalFunction, bool)`
- `MustRegisterFunction(fn LocalFunction)` for bootstrap

Duplicate registration:

- reject duplicates with explicit error

## 4. Validation Design

## 4.1 Config Validation

`NewCodeNode` must fail when:

- `function_ref` empty
- `inputs` empty
- `outputs` empty
- duplicate input names
- duplicate output names
- unsupported type expression
- input `required=true` with neither `value_selector` nor `default`

## 4.2 Type System (Phase 1)

Supported type expressions:

- `string`
- `number`
- `boolean`
- `object`
- `array<string>`
- `array<number>`
- `array<boolean>`
- `array<object>`

Type checking strategy:

- `number`: accept Go numeric types and JSON float64
- `object`: accept `map[string]interface{}`
- `array<T>`: accept `[]interface{}` and typed slices converted to `[]interface{}`

## 4.3 Input Assembly Rules

Per input binding:

1. Resolve from `value_selector` if present
2. If missing and `default` exists, use `default`
3. If still missing and `required=true`, fail with `CODE_NODE_INPUT_MISSING`
4. If value exists, validate type; mismatch -> `CODE_NODE_INPUT_TYPE_MISMATCH`

## 4.4 Output Validation Rules

Function result must be a map-like object:

- Missing required field -> `CODE_NODE_OUTPUT_MISSING`
- Declared field type mismatch -> `CODE_NODE_OUTPUT_TYPE_MISMATCH`
- Extra fields with strict mode -> `CODE_NODE_OUTPUT_SCHEMA_VIOLATION`

Returned node outputs:

- Only declared output fields (and validated)

## 5. Error Model

Introduce code-node scoped error wrapper:

```go
type CodeNodeError struct {
    Code    string
    Message string
    Cause   error
}
```

Node failure error string format:

- `[<CODE>] <message>`

Error code mapping:

- invalid config -> `CODE_NODE_INVALID_CONFIG`
- function missing -> `CODE_NODE_FUNCTION_NOT_FOUND`
- input missing -> `CODE_NODE_INPUT_MISSING`
- input type mismatch -> `CODE_NODE_INPUT_TYPE_MISMATCH`
- timeout -> `CODE_NODE_EXEC_TIMEOUT`
- execute error -> `CODE_NODE_EXEC_FAILED`
- output missing -> `CODE_NODE_OUTPUT_MISSING`
- output type mismatch -> `CODE_NODE_OUTPUT_TYPE_MISMATCH`
- strict schema violation -> `CODE_NODE_OUTPUT_SCHEMA_VIOLATION`

## 6. Runtime Execution Flow

1. `Run` starts and reads variable pool.
2. Build input map from DSL bindings.
3. Validate input map.
4. Resolve function by `FunctionRef`.
5. Create timeout context.
6. Execute local function.
7. Validate output.
8. Emit `NodeRunResult` with outputs + metadata.

Metadata proposal:

- `function_ref`
- `timeout_ms`
- `strict_schema`
- `elapsed_ms`
- `input_count`
- `output_count`

## 7. Concurrency and Safety

- Registry uses `sync.RWMutex`.
- Function execution is per node invocation; no shared mutable state in node object.
- Timeout cancellation relies on function respecting `ctx.Done()`.

## 8. File Change Plan

## 8.1 Update Existing Files

1. `internal/domain/workflow/node/code/code.go`
- Remove script executor path
- Use `function_ref` pipeline
- Add config/input/output validation integration

2. `internal/domain/workflow/engine/phase2_test.go`
- Replace old script-style code node test config with function-ref config
- Register test local functions

3. `internal/domain/workflow/engine/phase4_test.go`
- Replace old `language/code` usage in error-strategy tests with function-ref usage

## 8.2 Add New Files

1. `internal/domain/workflow/node/code/function_registry.go`
- LocalFunction interface
- thread-safe registry implementation

2. `internal/domain/workflow/node/code/schema_types.go`
- DSL structs for V2 input/output bindings
- supported type constants/parser

3. `internal/domain/workflow/node/code/validator.go`
- config, input, output validators

4. `internal/domain/workflow/node/code/errors.go`
- typed code node errors and error code constants

5. `internal/domain/workflow/node/code/code_v2_test.go`
- unit tests for validator, registry, runtime happy/fail paths

6. `internal/app/bootstrap/code_function_registry.go`
- register built-in local functions (initially minimal examples)

## 9. Testing Strategy

## 9.1 Unit Test Matrix

- registry: register/get/duplicate
- config validation: required fields/duplicates/unsupported type
- input validation: missing/required/default/type mismatch
- output validation: missing/type mismatch/extra fields strict mode
- timeout mapping: context deadline exceeded -> timeout error code

## 9.2 Integration Test Matrix

- workflow `start -> code -> end` success
- function not found
- input missing
- input type mismatch
- output missing required
- output type mismatch
- strict schema violation

## 9.3 Regression

- workflow engine event lifecycle unchanged
- non-code node tests remain green

## 10. Rollout Plan

1. Commit internal node refactor + tests.
2. Update examples to V2 code node style.
3. Update docs references to remove script execution language.
4. Run full test suite.

## 11. Open Decisions

- Default `timeout_ms`: 3000 vs 5000
- Strict schema default fixed true vs configurable globally
- Built-in function catalog scope for first release

## 12. Implementation Checklist

- [ ] Add registry implementation
- [ ] Add schema/type parser
- [ ] Add validators
- [ ] Refactor `code.go` runtime path
- [ ] Add typed error mapping
- [ ] Update engine tests for new DSL
- [ ] Add code node unit tests
- [ ] Add bootstrap function registration
- [ ] Update docs/examples
