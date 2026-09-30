package todolist

import "errors"

// ErrNotFound is returned when a todo ID is not present in the store.
var ErrNotFound = errors.New("todo not found")

// Store is an in-memory collection of todos keyed by ID.
type Store struct {
	nextID int
	items  map[int]Todo
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{nextID: 1, items: map[int]Todo{}}
}

// Add inserts a todo with the given title and priority, returning its new ID.
func (s *Store) Add(title string, priority Priority, tags ...string) int {
	id := s.nextID
	s.nextID++
	s.items[id] = Todo{ID: id, Title: title, Priority: priority, Tags: tags}
	return id
}

// Complete marks a todo done. It returns ErrNotFound if the ID is unknown.
func (s *Store) Complete(id int) error {
	todo, ok := s.items[id]
	if !ok {
		return ErrNotFound
	}
	todo.Done = true
	s.items[id] = todo
	return nil
}

// Remove deletes a todo. It returns ErrNotFound if the ID is unknown.
func (s *Store) Remove(id int) error {
	if _, ok := s.items[id]; !ok {
		return ErrNotFound
	}
	delete(s.items, id)
	return nil
}

// Count returns the number of todos in the store.
func (s *Store) Count() int {
	return len(s.items)
}

// All returns every todo, ordered by ascending ID.
func (s *Store) All() []Todo {
	out := make([]Todo, 0, len(s.items))
	for id := 1; id < s.nextID; id++ { // slopguard-ignore-mutant(boundary): Add never stores an ID >= nextID, so <= only adds a lookup that finds nothing
		if todo, ok := s.items[id]; ok {
			out = append(out, todo)
		}
	}
	return out
}
