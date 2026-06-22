package todolist

import (
	"errors"
	"testing"
)

func TestPriorityString(t *testing.T) {
	cases := map[Priority]string{Low: "low", Medium: "medium", High: "high", Priority(99): "low"}
	for p, want := range cases {
		if got := p.String(); got != want {
			t.Errorf("Priority(%d).String() = %q, want %q", p, got, want)
		}
	}
}

func TestTodoHasTag(t *testing.T) {
	todo := Todo{Tags: []string{"Home", "urgent"}}
	if !todo.HasTag("home") {
		t.Error("expected case-insensitive tag match")
	}
	if todo.HasTag("work") {
		t.Error("did not expect a match for absent tag")
	}
}

func TestStoreLifecycle(t *testing.T) {
	s := NewStore()
	a := s.Add("write tests", High, "dev")
	b := s.Add("ship it", Medium)
	if s.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", s.Count())
	}

	if err := s.Complete(a); err != nil {
		t.Fatalf("Complete(%d) failed: %v", a, err)
	}
	if err := s.Complete(999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Complete(unknown) = %v, want ErrNotFound", err)
	}

	all := s.All()
	if len(all) != 2 || !all[0].Done {
		t.Fatalf("All() unexpected: %+v", all)
	}

	if err := s.Remove(b); err != nil {
		t.Fatalf("Remove(%d) failed: %v", b, err)
	}
	if err := s.Remove(b); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove(removed) = %v, want ErrNotFound", err)
	}
	if s.Count() != 1 {
		t.Errorf("Count() = %d, want 1", s.Count())
	}
}

func TestFilterApply(t *testing.T) {
	todos := []Todo{
		{ID: 1, Title: "a", Priority: Low, Done: false, Tags: []string{"home"}},
		{ID: 2, Title: "b", Priority: High, Done: true, Tags: []string{"work"}},
		{ID: 3, Title: "c", Priority: Medium, Done: false, Tags: []string{"home", "work"}},
	}

	active := Active(todos)
	if len(active) != 2 {
		t.Fatalf("Active() = %d todos, want 2", len(active))
	}

	f := Filter{OnlyActive: true, MinPriority: Medium, RequiredTag: "work"}
	got := f.Apply(todos)
	if len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("Apply() = %+v, want only todo 3", got)
	}

	if len(Filter{}.Apply(todos)) != 3 {
		t.Error("zero Filter should match everything")
	}
}
