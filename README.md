# Todo CLI

A simple Go CLI todo app that stores todos in `~/.todo/todos.json` and syncs them via git.

## Install

```bash
go build -o todo .
sudo mv todo /usr/local/bin/
```

## Usage

- `todo init [remote-url]` — create `~/.todo` and initialize a git repository.
- `todo add [--priority high|medium|low] [--due yyyy-mm-dd] [--tags work,urgent] <text>` — add a todo.
- `todo edit <id> <text>` — edit a todo.
- `todo list [--all] [--priority value] [--tag value] [--due-before yyyy-mm-dd] [--sort id|created|due|priority] [--filter text]` — list todos.
- `todo done <id>` — mark a todo done and complete its subtasks.
- `todo delete <id>` — delete a todo.
- `todo subtask add <todo-id> <text>` — add a subtask.
- `todo subtask done <todo-id> <subtask-id>` — mark a subtask done.
- `todo subtask edit <todo-id> <subtask-id> <text>` — edit a subtask.
- `todo subtask delete <todo-id> <subtask-id>` — delete a subtask.
- `todo sync` — run `git pull --rebase` and `git push`.

## Data format

Todo items are stored in JSON at `~/.todo/todos.json`:

```json
[
  {
    "id": 1,
    "text": "Buy groceries",
    "done": false,
    "created_at": "2026-06-03T10:00:00Z",
    "priority": "high",
    "due_date": "2026-06-10",
    "tags": ["shopping", "urgent"],
    "subtasks": [
      {
        "id": 1,
        "text": "Milk",
        "done": false,
        "created_at": "2026-06-03T10:05:00Z"
      }
    ]
  }
]
```

## Git sync behavior

- Mutating commands save `todos.json`, stage it, commit with a descriptive message, and push to the configured remote.
- If push fails, the warning is printed but local state is preserved.
- `todo sync` runs `git pull --rebase` and then `git push`.

## Notes

- New fields are optional and omitted when empty.
- The CLI uses only Go standard library packages.

## Interactive editing and editor

- You can run `todo edit` with no arguments to interactively select a task using the arrow keys and press Enter to choose one.
- When the editor is needed, the CLI uses the `EDITOR` environment variable. If `EDITOR` is not set, `vi` is used by default. For example:

```bash
export EDITOR="code --wait"
todo edit 1
```
