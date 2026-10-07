package widgets

import (
	"fmt"
	"strings"
	"time"
)

func overrideProblems(w Instance, label, path string, entry Entry, known bool) []Problem {
	var problems []Problem
	at := entryReporter(&problems, w, label, path)
	if w.has("title") && strings.TrimSpace(w.Title) == "" {
		at("title", "title is empty: write one, or take the key off to show the widget's own")
	}
	if w.has("every") {
		if problem := EveryOverrideProblem(w.Every, entry, known); problem != "" {
			at("every", "%s", problem)
		}
	}
	return problems
}

// EveryOverrideProblem says what is wrong with every written on a board
// entry of entry's widget, or "" when it is fine. It takes what the
// widget's own source.every takes, and never less than that source's
// minimum. A prompt in a mode with no checks takes only manual. known is
// false for a type the catalog lacks: its source is unknown, so only the
// shape is checked.
func EveryOverrideProblem(every string, entry Entry, known bool) string {
	source := entry.Source
	if known && source.Kind == SourceCommand && source.Mode == ModeStream {
		return "a stream runs while the widget is on screen, so it takes no every"
	}
	provider := known && source.Kind == SourceProvider
	if every == EveryManual {
		if provider {
			return fmt.Sprintf("every manual: %s reads a provider, which runs on a schedule: write a duration like 60s", entry.Name)
		}
		return ""
	}
	d, err := time.ParseDuration(every)
	switch {
	case err != nil && provider:
		return fmt.Sprintf("every %q is not a duration: write it like 60s or 5m", every)
	case err != nil:
		return fmt.Sprintf("every %q is neither a duration nor manual: write it like 60s, 5m or manual", every)
	case !known:
		return ""
	}
	if problem := promptEveryProblem(source, every); problem != "" {
		return problem
	}
	minimum := minEveryOf(entry)
	if floor, err := time.ParseDuration(minimum); err == nil && d < floor {
		return fmt.Sprintf("every %s is below %s's minimum of %s", every, entry.Name, minimum)
	}
	return ""
}

func minEveryOf(entry Entry) string {
	c := CurrentContract()
	source := entry.Source
	switch {
	case source.Kind != SourceProvider:
		return c.Sources[source.Kind].MinEvery
	case entry.Provider != nil && entry.Provider.MinEvery != "":
		return entry.Provider.MinEvery
	case IsPackageProvider(source.Name):
		return c.PackageProvider.MinEvery
	}
	return c.Providers[source.Name].MinEvery
}
