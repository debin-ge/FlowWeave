package code

import (
	"fmt"
	"reflect"
	"strings"

	"flowweave/internal/domain/workflow/node"
)

var supportedBaseTypes = map[string]struct{}{
	"string":  {},
	"number":  {},
	"boolean": {},
	"object":  {},
}

func ValidateCodeNodeData(data *CodeNodeData) error {
	if data == nil {
		return NewError(CodeNodeInvalidConfig, "code node config is nil", nil)
	}
	if strings.TrimSpace(data.FunctionRef) == "" {
		return NewError(CodeNodeInvalidConfig, "function_ref is required", nil)
	}
	if len(data.Inputs) == 0 {
		return NewError(CodeNodeInvalidConfig, "inputs is required and cannot be empty", nil)
	}
	if len(data.Outputs) == 0 {
		return NewError(CodeNodeInvalidConfig, "outputs is required and cannot be empty", nil)
	}

	inputNames := make(map[string]struct{}, len(data.Inputs))
	for _, in := range data.Inputs {
		if strings.TrimSpace(in.Name) == "" {
			return NewError(CodeNodeInvalidConfig, "input name is required", nil)
		}
		if _, exists := inputNames[in.Name]; exists {
			return NewError(CodeNodeInvalidConfig, "duplicate input name: "+in.Name, nil)
		}
		inputNames[in.Name] = struct{}{}

		if err := ValidateTypeExpr(in.Type); err != nil {
			return err
		}
		if in.Required && len(in.ValueSelector) == 0 && in.Default == nil {
			return NewError(CodeNodeInvalidConfig, "required input needs value_selector or default: "+in.Name, nil)
		}
	}

	outputNames := make(map[string]struct{}, len(data.Outputs))
	for _, out := range data.Outputs {
		if strings.TrimSpace(out.Name) == "" {
			return NewError(CodeNodeInvalidConfig, "output name is required", nil)
		}
		if _, exists := outputNames[out.Name]; exists {
			return NewError(CodeNodeInvalidConfig, "duplicate output name: "+out.Name, nil)
		}
		outputNames[out.Name] = struct{}{}

		if err := ValidateTypeExpr(out.Type); err != nil {
			return err
		}
	}

	return nil
}

func ValidateTypeExpr(typeExpr string) error {
	t := strings.TrimSpace(typeExpr)
	if t == "" {
		return NewError(CodeNodeInvalidConfig, "type expression is required", nil)
	}

	if _, ok := supportedBaseTypes[t]; ok {
		return nil
	}

	if strings.HasPrefix(t, "array<") && strings.HasSuffix(t, ">") {
		inner := strings.TrimSuffix(strings.TrimPrefix(t, "array<"), ">")
		if _, ok := supportedBaseTypes[inner]; !ok {
			return NewError(CodeNodeInvalidConfig, "unsupported array inner type: "+inner, nil)
		}
		return nil
	}

	return NewError(CodeNodeInvalidConfig, "unsupported type expression: "+t, nil)
}

func BuildAndValidateInputs(vp node.VariablePoolAccessor, defs []CodeInputBinding) (map[string]interface{}, error) {
	inputs := make(map[string]interface{}, len(defs))

	for _, in := range defs {
		var (
			value  interface{}
			exists bool
		)

		if len(in.ValueSelector) > 0 && vp != nil {
			value, exists = vp.GetVariable(in.ValueSelector)
		}
		if !exists && in.Default != nil {
			value = in.Default
			exists = true
		}

		if !exists {
			if in.Required {
				return nil, NewError(CodeNodeInputMissing, "missing required input: "+in.Name, nil)
			}
			continue
		}

		if err := ValidateValueType(in.Type, value); err != nil {
			return nil, NewError(CodeNodeInputTypeMismatch, "input type mismatch: "+in.Name, err)
		}
		inputs[in.Name] = value
	}

	return inputs, nil
}

func ValidateAndFilterOutputs(raw map[string]interface{}, defs []CodeOutputBinding, strict bool) (map[string]interface{}, error) {
	if raw == nil {
		raw = map[string]interface{}{}
	}

	declared := make(map[string]CodeOutputBinding, len(defs))
	filtered := make(map[string]interface{}, len(defs))
	for _, out := range defs {
		declared[out.Name] = out
	}

	for _, out := range defs {
		value, exists := raw[out.Name]
		if !exists {
			if out.Required {
				return nil, NewError(CodeNodeOutputMissing, "missing required output: "+out.Name, nil)
			}
			continue
		}
		if err := ValidateValueType(out.Type, value); err != nil {
			return nil, NewError(CodeNodeOutputTypeMismatch, "output type mismatch: "+out.Name, err)
		}
		filtered[out.Name] = value
	}

	if strict {
		for k := range raw {
			if _, ok := declared[k]; !ok {
				return nil, NewError(CodeNodeOutputSchemaViolation, "extra output field not declared: "+k, nil)
			}
		}
	}

	return filtered, nil
}

func ValidateValueType(typeExpr string, value interface{}) error {
	if value == nil {
		return fmt.Errorf("value is nil")
	}

	t := strings.TrimSpace(typeExpr)
	if strings.HasPrefix(t, "array<") && strings.HasSuffix(t, ">") {
		inner := strings.TrimSuffix(strings.TrimPrefix(t, "array<"), ">")
		return validateArray(inner, value)
	}

	switch t {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
	case "number":
		if !isNumber(value) {
			return fmt.Errorf("expected number, got %T", value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", value)
		}
	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Errorf("expected object, got %T", value)
		}
	default:
		return fmt.Errorf("unsupported type expression: %s", typeExpr)
	}
	return nil
}

func validateArray(innerType string, value interface{}) error {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return fmt.Errorf("expected array, got %T", value)
	}

	for i := 0; i < rv.Len(); i++ {
		elem := rv.Index(i).Interface()
		switch innerType {
		case "string":
			if _, ok := elem.(string); !ok {
				return fmt.Errorf("expected array<string>, index %d got %T", i, elem)
			}
		case "number":
			if !isNumber(elem) {
				return fmt.Errorf("expected array<number>, index %d got %T", i, elem)
			}
		case "boolean":
			if _, ok := elem.(bool); !ok {
				return fmt.Errorf("expected array<boolean>, index %d got %T", i, elem)
			}
		case "object":
			if _, ok := elem.(map[string]interface{}); !ok {
				return fmt.Errorf("expected array<object>, index %d got %T", i, elem)
			}
		default:
			return fmt.Errorf("unsupported array inner type: %s", innerType)
		}
	}
	return nil
}

func isNumber(v interface{}) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	default:
		return false
	}
}
