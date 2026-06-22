package todolist

// Filter describes which todos to keep. A zero Filter matches everything.
type Filter struct {
	OnlyActive  bool
	MinPriority Priority
	RequiredTag string
}

// Apply returns the todos from the slice that satisfy every active criterion,
// preserving order.
func (f Filter) Apply(todos []Todo) []Todo {
	out := make([]Todo, 0, len(todos))
	for _, todo := range todos {
		if f.OnlyActive && todo.Done {
			continue
		}
		if todo.Priority < f.MinPriority {
			continue
		}
		if f.RequiredTag != "" && !todo.HasTag(f.RequiredTag) {
			continue
		}
		out = append(out, todo)
	}
	return out
}

// Active returns the todos that are not yet done.
func Active(todos []Todo) []Todo {
	return Filter{OnlyActive: true}.Apply(todos)
}
