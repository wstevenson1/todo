package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseTags(t *testing.T) {
	out := parseTags("work, urgent, ,personal")
	expected := []string{"work", "urgent", "personal"}
	if !reflect.DeepEqual(out, expected) {
		t.Fatalf("expected %#v got %#v", expected, out)
	}
}

func TestNextIDs(t *testing.T) {
	todos := []Todo{{ID: 3}, {ID: 1}, {ID: 7}}
	if want := 8; nextTodoID(todos) != want {
		t.Fatalf("expected %d got %d", want, nextTodoID(todos))
	}

	todo := &Todo{Subtasks: []Subtask{{ID: 2}, {ID: 5}}}
	if want := 6; nextSubtaskID(todo) != want {
		t.Fatalf("expected %d got %d", want, nextSubtaskID(todo))
	}
}

func TestMarkTodoDone(t *testing.T) {
	todo := &Todo{ID: 1, Text: "Task", Subtasks: []Subtask{{ID: 1, Text: "sub"}, {ID: 2, Text: "sub2"}}}
	markTodoDone(todo)
	if !todo.Done {
		t.Fatal("todo should be marked done")
	}
	if todo.DoneAt == nil {
		t.Fatal("todo done timestamp should be set")
	}
	if len(todo.Subtasks) != 2 {
		t.Fatal("todo should preserve subtasks")
	}
	for _, sub := range todo.Subtasks {
		if !sub.Done {
			t.Fatal("subtask should be marked done")
		}
		if sub.DoneAt == nil {
			t.Fatal("subtask done timestamp should be set")
		}
	}
}

func TestCompareDueDate(t *testing.T) {
	if !compareDueDate("2026-01-01", "2026-02-01") {
		t.Fatal("expected 2026-01-01 to come before 2026-02-01")
	}
	if compareDueDate("", "2026-01-01") {
		t.Fatal("empty due date should come after a real date")
	}
	if !compareDueDate("2026-01-01", "") {
		t.Fatal("real date should come before empty date")
	}
}

func TestMarshalJSON(t *testing.T) {
	todo := Todo{ID: 1, Text: "Task", CreatedAt: time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC), Priority: "high", DueDate: "2026-06-10", Tags: []string{"work"}}
	raw, err := json.Marshal(todo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "priority") || !strings.Contains(string(raw), "due_date") || !strings.Contains(string(raw), "tags") {
		t.Fatalf("expected json to contain new fields, got %s", raw)
	}
}
