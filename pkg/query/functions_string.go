package query

import (
	"fmt"
	"strings"
)

func init() {
	RegisterFunction("toLower", fnToLower)
	RegisterFunction("toUpper", fnToUpper)
	RegisterFunction("toString", fnToString)
	RegisterFunction("toBoolean", fnToBoolean)
	RegisterFunction("trim", fnTrim)
	RegisterFunction("replace", fnReplace)
	RegisterFunction("substring", fnSubstring)
	RegisterFunction("split", fnSplit)
	RegisterFunction("startsWith", fnStartsWith)
	RegisterFunction("endsWith", fnEndsWith)
	RegisterFunction("contains", fnContains)
	RegisterFunction("size", fnSize)
	RegisterFunction("length", fnLength)
}

func toString(val any) string {
	if val == nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", val)
}

func toInt64(val any) (int64, bool) {
	switch v := val.(type) {
	case int64:
		return v, true
	case float64:
		return int64(v), true
	case int:
		return int64(v), true
	default:
		return 0, false
	}
}

func fnToBoolean(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("toBoolean requires 1 argument")
	}
	if args[0] == nil {
		return nil, nil
	}
	switch v := args[0].(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(v) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return nil, nil
		}
	case int64:
		return v != 0, nil
	case float64:
		return v != 0, nil
	default:
		return nil, fmt.Errorf("toBoolean: unsupported type %T", args[0])
	}
}

func fnToLower(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("toLower requires 1 argument")
	}
	return strings.ToLower(toString(args[0])), nil
}

func fnToUpper(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("toUpper requires 1 argument")
	}
	return strings.ToUpper(toString(args[0])), nil
}

func fnToString(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("toString requires 1 argument")
	}
	return toString(args[0]), nil
}

func fnTrim(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("trim requires 1 argument")
	}
	return strings.TrimSpace(toString(args[0])), nil
}

func fnReplace(args []any) (any, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("replace requires 3 arguments (string, old, new)")
	}
	return strings.ReplaceAll(toString(args[0]), toString(args[1]), toString(args[2])), nil
}

func fnSubstring(args []any) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("substring requires 2-3 arguments (string, start[, length])")
	}
	s := toString(args[0])
	start, ok := toInt64(args[1])
	if !ok {
		return nil, fmt.Errorf("substring: start must be numeric")
	}

	if start < 0 {
		start = 0
	}
	if int(start) >= len(s) {
		return "", nil
	}

	if len(args) >= 3 {
		length, ok := toInt64(args[2])
		if !ok {
			return nil, fmt.Errorf("substring: length must be numeric")
		}
		end := int(start + length)
		if end > len(s) {
			end = len(s)
		}
		return s[start:end], nil
	}

	return s[start:], nil
}

func fnSplit(args []any) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("split requires 2 arguments (string, delimiter)")
	}
	parts := strings.Split(toString(args[0]), toString(args[1]))
	result := make([]any, len(parts))
	for i, p := range parts {
		result[i] = p
	}
	return result, nil
}

func fnStartsWith(args []any) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("startsWith requires 2 arguments")
	}
	return strings.HasPrefix(toString(args[0]), toString(args[1])), nil
}

func fnEndsWith(args []any) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("endsWith requires 2 arguments")
	}
	return strings.HasSuffix(toString(args[0]), toString(args[1])), nil
}

func fnContains(args []any) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("contains requires 2 arguments")
	}
	return strings.Contains(toString(args[0]), toString(args[1])), nil
}

func fnSize(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("size requires 1 argument")
	}
	val := args[0]
	if val == nil {
		return int64(0), nil
	}
	switch v := val.(type) {
	case string:
		return int64(len(v)), nil
	case []any:
		return int64(len(v)), nil
	default:
		return int64(0), nil
	}
}

func fnLength(args []any) (any, error) {
	// length is an alias for size on strings
	return fnSize(args)
}
