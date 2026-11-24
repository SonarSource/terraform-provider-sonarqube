package schemacheck

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Change is a schema change that can break the configuration or the state of
// a user.
type Change struct {
	Schema  string
	Path    string
	Message string
}

func (c Change) String() string {
	if c.Path == "" {
		return c.Schema + ": " + c.Message
	}
	return c.Schema + ": " + c.Path + ": " + c.Message
}

// Compare gives the breaking changes from base to head, sorted.
func Compare(base, head Snapshot) []Change {
	var changes []Change
	for _, name := range sortedNames(base) {
		headSchema, ok := head[name]
		if !ok {
			changes = append(changes, Change{Schema: name, Message: "removed"})
			continue
		}
		changes = append(changes, compareSchema(name, base[name], headSchema)...)
	}
	return changes
}

func compareSchema(name string, base, head Schema) []Change {
	var changes []Change
	add := func(path string, messages ...string) {
		for _, m := range messages {
			changes = append(changes, Change{Schema: name, Path: path, Message: m})
		}
	}

	for _, path := range sortedPaths(base) {
		h, ok := head[path]
		switch {
		case ok && !parentChanged(path, base, head):
			add(path, compareAttribute(base[path], h)...)
		case !parentChanged(path, base, head):
			add(path, "removed")
		}
	}

	for _, path := range sortedPaths(head) {
		_, inBase := base[path]
		// A required attribute in a new optional parent breaks nothing.
		if !inBase && head[path].Required && !parentChanged(path, base, head) {
			add(path, "new required attribute")
		}
	}

	return changes
}

func compareAttribute(b, h Attribute) []string {
	if b.Kind != h.Kind {
		return []string{fmt.Sprintf("type changed from %s to %s", b.Kind, h.Kind)}
	}

	var messages []string
	if !b.Required && h.Required {
		messages = append(messages, "now required")
	}
	if (b.Required || b.Optional) && !h.Required && !h.Optional {
		messages = append(messages, "can no longer be set in the configuration")
	}
	if !b.Sensitive && h.Sensitive {
		messages = append(messages, "now sensitive, so an output that shows it must set sensitive = true")
	}
	if b.Computed && !h.Computed {
		messages = append(messages, "no longer computed, so a value that the server set is planned as null")
	}
	if !b.WriteOnly && h.WriteOnly {
		messages = append(messages, "now write-only, so a reference to it gives null")
	}
	// A default never applies to a configuration that had to set the attribute.
	if !b.Required && b.Default != h.Default {
		messages = append(messages, fmt.Sprintf("default changed from %q to %q", b.Default, h.Default))
	}
	return append(messages, comparePlanModifiers(b.PlanModifiers, h.PlanModifiers)...)
}

func comparePlanModifiers(base, head []PlanModifier) []string {
	var messages []string
	for _, m := range head {
		switch {
		case slices.ContainsFunc(base, m.same):
		case m.Replace:
			messages = append(messages, fmt.Sprintf("now requires replacement (%s)", m.Description))
		default:
			messages = append(messages, fmt.Sprintf("new plan modifier %s (%s)", m.Type, m.Description))
		}
	}
	return messages
}

func (m PlanModifier) same(o PlanModifier) bool {
	return m.Type == o.Type
}

// parentChanged tells whether a parent of path is missing from base or head,
// or has a different kind. The change of the parent is then the only change
// to report.
func parentChanged(path string, base, head Schema) bool {
	for i := strings.LastIndex(path, "."); i >= 0; i = strings.LastIndex(path[:i], ".") {
		b, inBase := base[path[:i]]
		h, inHead := head[path[:i]]
		if !inBase || !inHead || b.Kind != h.Kind {
			return true
		}
	}
	return false
}

func sortedNames(s Snapshot) []string {
	names := make([]string, 0, len(s))
	for name := range s {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedPaths(s Schema) []string {
	paths := make([]string, 0, len(s))
	for path := range s {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
