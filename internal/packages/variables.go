package packages

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The types a variable may declare. They are the names `packages list`
// already printed for a default's shape, so a type read off a default and a
// type a package wrote are the same word.
const (
	TypeString  = "string"
	TypeBoolean = "boolean"
	TypeNumber  = "number"
	TypeList    = "list"
	TypeMap     = "map"
)

// KnownTypes is every value `type` accepts on a variable.
var KnownTypes = []string{TypeString, TypeBoolean, TypeNumber, TypeList, TypeMap}

// fieldTypes are the types a field of a list entry may have: one level of
// structure is what a list of things needs, and a nested one would be a
// second configuration language.
var fieldTypes = []string{TypeString, TypeBoolean, TypeNumber}

// TypeOf names the shape of a value read from YAML, or "" for nothing.
func TypeOf(value any) string {
	switch value.(type) {
	case string:
		return TypeString
	case bool:
		return TypeBoolean
	case int, int64, uint64, float64:
		return TypeNumber
	case []any:
		return TypeList
	case map[string]any:
		return TypeMap
	default:
		return ""
	}
}

// Check says why value does not fit the variable, or nil when it does. A
// variable without a type takes anything.
func (v Variable) Check(value any) error {
	if v.Type == "" {
		return nil
	}
	if err := checkType(v.Type, value); err != nil {
		return err
	}
	if v.Type != TypeList || v.Items == nil || len(v.Items.Fields) == 0 {
		return nil
	}
	for i, entry := range value.([]any) {
		if err := v.Items.check(entry); err != nil {
			return fmt.Errorf("[%d]%w", i, err)
		}
	}
	return nil
}

// check reads one list entry. The error starts with the separator that
// follows the entry's index, so the caller can prefix it.
func (items Items) check(entry any) error {
	names := slices.Sorted(maps.Keys(items.Fields))
	fields, ok := entry.(map[string]any)
	if !ok {
		return fmt.Errorf(": each entry is a mapping of %s, got %s", strings.Join(names, ", "), described(entry))
	}
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		field, known := items.Fields[key]
		if !known {
			return fmt.Errorf(": %q is not a field of this list: it takes %s", key, strings.Join(names, ", "))
		}
		if err := checkType(field.kind(), fields[key]); err != nil {
			return fmt.Errorf(".%s: %w", key, err)
		}
	}
	for _, name := range names {
		if _, set := fields[name]; items.Fields[name].Required && !set {
			return fmt.Errorf(": %q is required", name)
		}
	}
	return nil
}

func (f Field) kind() string {
	if f.Type == "" {
		return TypeString
	}
	return f.Type
}

func checkType(kind string, value any) error {
	if TypeOf(value) == kind {
		return nil
	}
	return fmt.Errorf("a %s, got %s", kind, described(value))
}

// described says what a value is, quoting a short scalar so the person can
// find the one they wrote.
func described(value any) string {
	switch v := value.(type) {
	case nil:
		return "nothing"
	case string:
		return fmt.Sprintf("the string %q", v)
	case bool:
		return fmt.Sprintf("the boolean %t", v)
	case int, int64, uint64, float64:
		return fmt.Sprintf("the number %v", v)
	case []any:
		return "a list"
	case map[string]any:
		return "a map"
	default:
		return fmt.Sprintf("%T", value)
	}
}

// validateVariables reports a type nobody can check against, an items block
// that cannot apply, and a default that does not fit its own type.
func validateVariables(m Manifest) []Problem {
	var problems []Problem
	at := func(what string) {
		problems = append(problems, Problem{File: FileName, Line: m.Lines["variables"], What: what})
	}
	for _, name := range slices.Sorted(maps.Keys(m.Variables)) {
		v := m.Variables[name]
		if v.Type != "" && !slices.Contains(KnownTypes, v.Type) {
			at(fmt.Sprintf("variable %q: type %q, and a type is one of %s", name, v.Type, strings.Join(KnownTypes, ", ")))
			continue
		}
		if v.Items != nil {
			if v.Type != TypeList {
				at(fmt.Sprintf("variable %q: items describes the entries of a list, and this is a %s",
					name, orUntyped(v.Type)))
				continue
			}
			if len(v.Items.Fields) == 0 {
				at(fmt.Sprintf("variable %q: items.fields is empty: name the fields each entry has, or drop items", name))
				continue
			}
			if bad := badFields(v.Items); len(bad) > 0 {
				for _, what := range bad {
					at(fmt.Sprintf("variable %q: %s", name, what))
				}
				continue
			}
		}
		if v.Default == nil {
			continue
		}
		if err := v.Check(v.Default); err != nil {
			at(fmt.Sprintf("variable %q: the default is not its own type: %v", name, err))
		}
	}
	return problems
}

func badFields(items *Items) []string {
	var out []string
	for _, field := range slices.Sorted(maps.Keys(items.Fields)) {
		if kind := items.Fields[field].Type; kind != "" && !slices.Contains(fieldTypes, kind) {
			out = append(out, fmt.Sprintf("field %q has type %q, and a field is one of %s",
				field, kind, strings.Join(fieldTypes, ", ")))
		}
	}
	return out
}

func orUntyped(kind string) string {
	if kind == "" {
		return "variable without a type"
	}
	return kind
}
