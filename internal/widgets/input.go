package widgets

import (
	"fmt"
	"slices"
	"strings"
)

var inputTypes = []string{"string", "number", "boolean", InputChoice}

func checkInput(name string, input Input, c Contract, at func(format string, args ...any)) {
	if !inputName.MatchString(name) {
		at("input %q: use lower case letters, digits and underscores, starting with a letter", name)
	}
	switch {
	case !slices.Contains(inputTypes, input.Type):
		at("input %q has type %q: the types are %s", name, input.Type, strings.Join(inputTypes, ", "))
	case input.Type == InputChoice:
		checkChoice(name, input, c, at)
	default:
		if input.choiceKeys {
			at("input %q is a %s: from and many belong to a choice", name, input.Type)
		}
		if input.Default != nil && !valueFits(input.Type, input.Default) {
			at("input %q is a %s, and its default %v is not", name, input.Type, input.Default)
		}
	}
}

func checkChoice(name string, input Input, c Contract, at func(format string, args ...any)) {
	sources := strings.Join(sortedKeys(c.OptionSources), ", ")
	if _, known := c.OptionSources[input.From]; input.From == "" {
		at("input %q is a choice, and needs from: one of %s", name, sources)
	} else if !known {
		at("input %q takes its options from %q, which is not a source: the sources are %s", name, input.From, sources)
	}
	switch {
	case input.manyProblem != "":
		at("input %q: %s", name, input.manyProblem)
	case input.Default == nil || InputValueFits(input, input.Default):
	case input.Many:
		at("input %q is a list of choices, and its default %s is not: write [a, b], or [] for all", name, shownValue(input.Default))
	default:
		at("input %q is one choice, and its default %s is not a name", name, shownValue(input.Default))
	}
}

func needsChoiceEngine(w Widget) bool {
	constraint, err := parseEngineConstraint(w.Requires.Engine)
	if err != nil || !constraint.allows(LastEngineWithoutChoices) {
		return false
	}
	for _, input := range w.Inputs {
		if input.Type == InputChoice {
			return true
		}
	}
	return false
}

// InputValueFits says whether value has the shape input takes: one name, or
// a list of names for a choice of many, else a value of the input's type.
func InputValueFits(input Input, value any) bool {
	if input.Type != InputChoice {
		return valueFits(input.Type, value)
	}
	if input.Many {
		_, ok := nameList(value)
		return ok
	}
	_, ok := value.(string)
	return ok
}

func nameList(value any) ([]string, bool) {
	switch list := value.(type) {
	case []string:
		return list, true
	case []any:
		names := make([]string, 0, len(list))
		for _, item := range list {
			name, ok := item.(string)
			if !ok {
				return nil, false
			}
			names = append(names, name)
		}
		return names, true
	}
	return nil, false
}

func shownValue(value any) string {
	if names, ok := nameList(value); ok {
		return "[" + strings.Join(names, ", ") + "]"
	}
	return fmt.Sprint(value)
}
