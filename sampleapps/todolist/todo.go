// Package todolist is a small, fully-tested reference fixture used to
// regression-test the slopguard-go analyzer. CI asserts its wCRAP report stays
// stable (clean code, high coverage, zero crappy methods) — drift signals an
// analyzer bug.
package todolist

import "strings"

// Priority ranks a todo's urgency.
type Priority int

const (
	Low Priority = iota
	Medium
	High
)

// String renders a priority as a lowercase label.
func (p Priority) String() string {
	switch p {
	case High:
		return "high"
	case Medium:
		return "medium"
	default:
		return "low"
	}
}

// Todo is a single task.
type Todo struct {
	ID       int
	Title    string
	Priority Priority
	Done     bool
	Tags     []string
}

// HasTag reports whether the todo carries the given tag, case-insensitively.
func (t Todo) HasTag(tag string) bool {
	for _, existing := range t.Tags {
		if strings.EqualFold(existing, tag) {
			return true
		}
	}
	return false
}
