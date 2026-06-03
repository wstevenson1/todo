package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const todoDirName = ".todo"
const todoFileName = "todos.json"

var now = time.Now

func main() {
	if len(os.Args) < 2 {
		printUsageAndExit("missing command")
	}

	cmd := os.Args[1]
	switch cmd {
	case "init":
		handleInit(os.Args[2:])
	case "add":
		handleAdd(os.Args[2:])
	case "edit":
		handleEdit(os.Args[2:])
	case "list":
		handleList(os.Args[2:])
	case "done":
		handleDone(os.Args[2:])
	case "delete":
		handleDelete(os.Args[2:])
	case "subtask":
		handleSubtask(os.Args[2:])
	case "sync":
		handleSync(os.Args[2:])
	default:
		printUsageAndExit(fmt.Sprintf("unknown command %q", cmd))
	}
}

func printUsageAndExit(message string) {
	if message != "" {
		fmt.Fprintf(os.Stderr, "error: %s\n\n", message)
	}
	fmt.Fprint(os.Stderr, `todo - simple git-backed todo list

Usage:
  todo init [remote-url]
  todo add [--priority value] [--due yyyy-mm-dd] [--tags a,b] <text>
  todo edit <id> <text>
  todo list [--all] [--priority value] [--tag value] [--due-before yyyy-mm-dd] [--sort id|created|due|priority] [--filter text]
  todo done <id>
  todo delete <id>
  todo subtask add <todo-id> <text>
  todo subtask done <todo-id> <subtask-id>
  todo subtask edit <todo-id> <subtask-id> <text>
  todo subtask delete <todo-id> <subtask-id>
  todo sync

Flags:
  --priority   Todo priority: low, medium, high
  --due        Due date in yyyy-mm-dd format
  --tags       Comma-separated tags list
  --all        Show completed todos too
  --tag        Filter by tag
  --filter     Search text in todo and subtasks
  --due-before Filter by due date at or before yyyy-mm-dd
  --sort       Sort todos by id, created, due, or priority
`)
	os.Exit(1)
}

type Todo struct {
	ID        int        `json:"id"`
	Text      string     `json:"text"`
	Done      bool       `json:"done"`
	CreatedAt time.Time  `json:"created_at"`
	DoneAt    *time.Time `json:"done_at,omitempty"`
	Priority  string     `json:"priority,omitempty"`
	DueDate   string     `json:"due_date,omitempty"`
	Tags      []string   `json:"tags,omitempty"`
	Subtasks  []Subtask  `json:"subtasks,omitempty"`
}

type Subtask struct {
	ID        int        `json:"id"`
	Text      string     `json:"text"`
	Done      bool       `json:"done"`
	CreatedAt time.Time  `json:"created_at"`
	DoneAt    *time.Time `json:"done_at,omitempty"`
}

func handleInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	fs.Usage = func() { printUsageAndExit("usage: todo init [remote-url]") }
	_ = fs.Parse(args)

	remoteURL := ""
	if fs.NArg() > 0 {
		remoteURL = fs.Arg(0)
	}

	if err := ensureTodoDir(); err != nil {
		fatal(err)
	}

	if !gitRepoInitialized() {
		if _, err := runGitOutput("init"); err != nil {
			fatal(fmt.Errorf("git init failed: %w", err))
		}
	}

	if err := ensureTodosFile(); err != nil {
		fatal(err)
	}

	if remoteURL != "" {
		if err := setGitRemote(remoteURL); err != nil {
			fatal(err)
		}
	}

	if err := commitAndPush("Initialize todo repository"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s\n", err)
	}

	fmt.Println("Initialized ~/.todo repository")
}

func handleAdd(args []string) {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	priority := fs.String("priority", "", "todo priority")
	due := fs.String("due", "", "due date yyyy-mm-dd")
	tags := fs.String("tags", "", "comma-separated tags")
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		printUsageAndExit("todo add requires text")
	}

	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if text == "" {
		fatal(errors.New("todo text cannot be empty"))
	}

	todos, err := loadTodos()
	if err != nil {
		fatal(err)
	}

	todo := Todo{
		ID:        nextTodoID(todos),
		Text:      text,
		Done:      false,
		CreatedAt: now().UTC(),
		Priority:  strings.TrimSpace(*priority),
		DueDate:   strings.TrimSpace(*due),
		Tags:      parseTags(*tags),
	}

	todos = append(todos, todo)
	if err := saveTodos(todos); err != nil {
		fatal(err)
	}

	message := fmt.Sprintf("Add todo #%d: %s", todo.ID, todo.Text)
	if err := commitAndPush(message); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s\n", err)
	}

	fmt.Printf("Added todo #%d\n", todo.ID)
}

func handleEdit(args []string) {
	// Support three modes:
	// 1) `todo edit <id> <text>` - inline edit
	// 2) `todo edit <id>` - open $EDITOR to edit the todo text
	// 3) `todo edit` - interactive selection via arrow keys, then open editor

	if len(args) == 0 {
		// interactive selection
		todos, err := loadTodos()
		if err != nil {
			fatal(err)
		}
		selID, err := chooseTodoInteractive(todos)
		if err != nil {
			fatal(err)
		}
		if selID <= 0 {
			fmt.Println("No selection")
			return
		}
		args = []string{strconv.Itoa(selID)}
	}

	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		fatal(errors.New("invalid todo id"))
	}

	todos, err := loadTodos()
	if err != nil {
		fatal(err)
	}

	todo, index := findTodo(todos, id)
	if todo == nil {
		fatal(fmt.Errorf("todo %d not found", id))
	}

	// If additional args present, treat as inline text
	if len(args) >= 2 {
		text := strings.TrimSpace(strings.Join(args[1:], " "))
		if text == "" {
			fatal(errors.New("todo text cannot be empty"))
		}
		todos[index].Text = text
		if err := saveTodos(todos); err != nil {
			fatal(err)
		}
		message := fmt.Sprintf("Update todo #%d text", id)
		if err := commitAndPush(message); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s\n", err)
		}
		fmt.Printf("Edited todo #%d\n", id)
		return
	}

	// No inline text: open editor
	editorText := todo.Text
	edited, err := openEditor(editorText)
	if err != nil {
		fatal(fmt.Errorf("editor failed: %w", err))
	}
	edited = strings.TrimSpace(edited)
	if edited == "" {
		fmt.Println("Edit cancelled or empty text; no changes made")
		return
	}
	todos[index].Text = edited
	if err := saveTodos(todos); err != nil {
		fatal(err)
	}
	message := fmt.Sprintf("Update todo #%d text (editor)", id)
	if err := commitAndPush(message); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s\n", err)
	}
	fmt.Printf("Edited todo #%d\n", id)
}

// chooseTodoInteractive renders a simple interactive list where the user can
// move with arrow keys and press Enter to select. Returns the selected todo ID.
func chooseTodoInteractive(todos []Todo) (int, error) {
	if len(todos) == 0 {
		return -1, nil
	}

	// Save current stty settings
	oldStateBytes, _ := exec.Command("stty", "-g").Output()
	oldState := strings.TrimSpace(string(oldStateBytes))
	// Put terminal in raw mode
	_ = exec.Command("stty", "raw", "-echo").Run()
	defer func() {
		if oldState != "" {
			_ = exec.Command("stty", oldState).Run()
		} else {
			_ = exec.Command("stty", "-raw", "echo").Run()
		}
	}()

	sel := 0
	reader := bufio.NewReader(os.Stdin)
	// initial render
	for {
		// clear screen
		fmt.Print("\x1b[H\x1b[2J")
		fmt.Println("Select a todo (use ↑/↓, Enter to choose, q to cancel):")
		for i, t := range todos {
			marker := "   "
			if i == sel {
				marker = ">  "
			}
			status := "[ ]"
			if t.Done {
				status = "[x]"
			}
			meta := ""
			if t.Priority != "" {
				meta = " priority:" + t.Priority
			}
			if t.DueDate != "" {
				meta += " due:" + t.DueDate
			}
			fmt.Printf("%s%d. %s %s%s\n", marker, t.ID, status, t.Text, meta)
		}

		// read a byte
		b, err := reader.ReadByte()
		if err != nil {
			return -1, err
		}
		if b == 'q' || b == 'Q' {
			return -1, nil
		}
		if b == '\r' || b == '\n' {
			return todos[sel].ID, nil
		}
		if b == 0x1b {
			// possible escape sequence
			b2, err := reader.ReadByte()
			if err != nil {
				continue
			}
			if b2 == '[' || b2 == 'O' {
				b3, err := reader.ReadByte()
				if err != nil {
					continue
				}
				switch b3 {
				case 'A': // up
					if sel > 0 {
						sel--
					}
				case 'B': // down
					if sel < len(todos)-1 {
						sel++
					}
				case 'C': // right - ignore
				case 'D': // left - ignore
				}
			}
		}
	}
}

// openEditor opens the user's $EDITOR (or vi by default) on a temporary file
// prepopulated with initial, and returns the edited content.
func openEditor(initial string) (string, error) {
	editor := os.Getenv("EDITOR")
	if strings.TrimSpace(editor) == "" {
		editor = "vi"
	}

	tmpf, err := os.CreateTemp("", "todo-edit-*.txt")
	if err != nil {
		return "", err
	}
	tmpPath := tmpf.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpf.WriteString(initial); err != nil {
		tmpf.Close()
		return "", err
	}
	tmpf.Close()

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}

	out, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func handleList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	showAll := fs.Bool("all", false, "show completed todos")
	priority := fs.String("priority", "", "filter by priority")
	tag := fs.String("tag", "", "filter by tag")
	dueBefore := fs.String("due-before", "", "show todos due on or before date")
	sortBy := fs.String("sort", "id", "sort field")
	filter := fs.String("filter", "", "search text")
	_ = fs.Parse(args)

	todos, err := loadTodos()
	if err != nil {
		fatal(err)
	}

	filtered, err := filterTodos(todos, *showAll, *priority, *tag, *dueBefore, *filter)
	if err != nil {
		fatal(err)
	}

	sortTodos(filtered, *sortBy)
	printTodos(filtered, *showAll)
}

func handleDone(args []string) {
	if len(args) != 1 {
		printUsageAndExit("todo done requires an id")
	}

	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		fatal(errors.New("invalid todo id"))
	}

	todos, err := loadTodos()
	if err != nil {
		fatal(err)
	}

	todo, index := findTodo(todos, id)
	if todo == nil {
		fatal(fmt.Errorf("todo %d not found", id))
	}

	markTodoDone(&todos[index])
	if err := saveTodos(todos); err != nil {
		fatal(err)
	}

	message := fmt.Sprintf("Mark todo #%d done", id)
	if err := commitAndPush(message); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s\n", err)
	}

	fmt.Printf("Todo #%d marked done\n", id)
}

func handleDelete(args []string) {
	if len(args) != 1 {
		printUsageAndExit("todo delete requires an id")
	}

	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		fatal(errors.New("invalid todo id"))
	}

	todos, err := loadTodos()
	if err != nil {
		fatal(err)
	}

	_, index := findTodo(todos, id)
	if index < 0 {
		fatal(fmt.Errorf("todo %d not found", id))
	}

	todos = append(todos[:index], todos[index+1:]...)
	if err := saveTodos(todos); err != nil {
		fatal(err)
	}

	message := fmt.Sprintf("Delete todo #%d", id)
	if err := commitAndPush(message); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s\n", err)
	}

	fmt.Printf("Deleted todo #%d\n", id)
}

func handleSubtask(args []string) {
	if len(args) < 1 {
		printUsageAndExit("todo subtask requires a subcommand")
	}

	subcmd := args[0]
	switch subcmd {
	case "add":
		handleSubtaskAdd(args[1:])
	case "done":
		handleSubtaskDone(args[1:])
	case "edit":
		handleSubtaskEdit(args[1:])
	case "delete":
		handleSubtaskDelete(args[1:])
	default:
		printUsageAndExit(fmt.Sprintf("unknown subtask command %q", subcmd))
	}
}

func handleSubtaskAdd(args []string) {
	if len(args) < 2 {
		printUsageAndExit("todo subtask add requires todo-id and text")
	}

	todoID, err := strconv.Atoi(args[0])
	if err != nil || todoID <= 0 {
		fatal(errors.New("invalid todo id"))
	}

	text := strings.TrimSpace(strings.Join(args[1:], " "))
	if text == "" {
		fatal(errors.New("subtask text cannot be empty"))
	}

	todos, err := loadTodos()
	if err != nil {
		fatal(err)
	}

	todo, index := findTodo(todos, todoID)
	if todo == nil {
		fatal(fmt.Errorf("todo %d not found", todoID))
	}

	subtask := Subtask{
		ID:        nextSubtaskID(todo),
		Text:      text,
		Done:      false,
		CreatedAt: now().UTC(),
	}

	todos[index].Subtasks = append(todos[index].Subtasks, subtask)
	if err := saveTodos(todos); err != nil {
		fatal(err)
	}

	message := fmt.Sprintf("Add subtask #%d to todo #%d", subtask.ID, todoID)
	if err := commitAndPush(message); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s\n", err)
	}

	fmt.Printf("Added subtask #%d to todo #%d\n", subtask.ID, todoID)
}

func handleSubtaskDone(args []string) {
	if len(args) != 2 {
		printUsageAndExit("todo subtask done requires todo-id and subtask-id")
	}

	todoID, err := strconv.Atoi(args[0])
	if err != nil || todoID <= 0 {
		fatal(errors.New("invalid todo id"))
	}
	subID, err := strconv.Atoi(args[1])
	if err != nil || subID <= 0 {
		fatal(errors.New("invalid subtask id"))
	}

	todos, err := loadTodos()
	if err != nil {
		fatal(err)
	}

	todo, todoIndex := findTodo(todos, todoID)
	if todo == nil {
		fatal(fmt.Errorf("todo %d not found", todoID))
	}

	subtask, subIndex := findSubtask(todo, subID)
	if subtask == nil {
		fatal(fmt.Errorf("subtask %d not found on todo %d", subID, todoID))
	}

	markSubtaskDone(&todos[todoIndex].Subtasks[subIndex])
	if allSubtasksDone(todos[todoIndex].Subtasks) {
		markTodoDone(&todos[todoIndex])
	}

	if err := saveTodos(todos); err != nil {
		fatal(err)
	}

	message := fmt.Sprintf("Mark subtask #%d done on todo #%d", subID, todoID)
	if err := commitAndPush(message); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s\n", err)
	}

	fmt.Printf("Subtask #%d on todo #%d marked done\n", subID, todoID)
}

func handleSubtaskEdit(args []string) {
	if len(args) < 3 {
		printUsageAndExit("todo subtask edit requires todo-id, subtask-id, and text")
	}

	todoID, err := strconv.Atoi(args[0])
	if err != nil || todoID <= 0 {
		fatal(errors.New("invalid todo id"))
	}
	subID, err := strconv.Atoi(args[1])
	if err != nil || subID <= 0 {
		fatal(errors.New("invalid subtask id"))
	}

	text := strings.TrimSpace(strings.Join(args[2:], " "))
	if text == "" {
		fatal(errors.New("subtask text cannot be empty"))
	}

	todos, err := loadTodos()
	if err != nil {
		fatal(err)
	}

	todo, todoIndex := findTodo(todos, todoID)
	if todo == nil {
		fatal(fmt.Errorf("todo %d not found", todoID))
	}

	_, subIndex := findSubtask(todo, subID)
	if subIndex < 0 {
		fatal(fmt.Errorf("subtask %d not found on todo %d", subID, todoID))
	}

	todos[todoIndex].Subtasks[subIndex].Text = text
	if err := saveTodos(todos); err != nil {
		fatal(err)
	}

	message := fmt.Sprintf("Update subtask #%d on todo #%d", subID, todoID)
	if err := commitAndPush(message); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s\n", err)
	}

	fmt.Printf("Edited subtask #%d on todo #%d\n", subID, todoID)
}

func handleSubtaskDelete(args []string) {
	if len(args) != 2 {
		printUsageAndExit("todo subtask delete requires todo-id and subtask-id")
	}

	todoID, err := strconv.Atoi(args[0])
	if err != nil || todoID <= 0 {
		fatal(errors.New("invalid todo id"))
	}
	subID, err := strconv.Atoi(args[1])
	if err != nil || subID <= 0 {
		fatal(errors.New("invalid subtask id"))
	}

	todos, err := loadTodos()
	if err != nil {
		fatal(err)
	}

	todo, todoIndex := findTodo(todos, todoID)
	if todo == nil {
		fatal(fmt.Errorf("todo %d not found", todoID))
	}

	_, subIndex := findSubtask(todo, subID)
	if subIndex < 0 {
		fatal(fmt.Errorf("subtask %d not found on todo %d", subID, todoID))
	}

	todos[todoIndex].Subtasks = append(todos[todoIndex].Subtasks[:subIndex], todos[todoIndex].Subtasks[subIndex+1:]...)
	if err := saveTodos(todos); err != nil {
		fatal(err)
	}

	message := fmt.Sprintf("Delete subtask #%d from todo #%d", subID, todoID)
	if err := commitAndPush(message); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s\n", err)
	}

	fmt.Printf("Deleted subtask #%d from todo #%d\n", subID, todoID)
}

func handleSync(args []string) {
	if len(args) != 0 {
		printUsageAndExit("todo sync does not accept arguments")
	}

	if !gitRepoInitialized() {
		fatal(errors.New("repository not initialized, run todo init"))
	}

	if !gitHasRemote() {
		fatal(errors.New("no remote configured, run todo init <remote-url> or add a remote"))
	}

	if _, err := runGitOutput("pull", "--rebase"); err != nil {
		fatal(fmt.Errorf("git pull --rebase failed: %w", err))
	}

	if _, err := runGitOutput("push"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: git push failed: %s\n", err)
	}

	fmt.Println("Sync complete")
}

func ensureTodoDir() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(home, todoDirName), 0o755)
}

func ensureTodosFile() error {
	if err := ensureTodoDir(); err != nil {
		return err
	}
	path, err := todoFilePath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, []byte("[]\n"), 0o644)
}

func todoFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, todoDirName, todoFileName), nil
}

func loadTodos() ([]Todo, error) {
	path, err := todoFilePath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Todo{}, nil
		}
		return nil, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(raw) == 0 {
		return []Todo{}, nil
	}

	var todos []Todo
	if err := json.Unmarshal(raw, &todos); err != nil {
		return nil, err
	}
	return todos, nil
}

func saveTodos(todos []Todo) error {
	if err := ensureTodoDir(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(todos, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	path, err := todoFilePath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func nextTodoID(todos []Todo) int {
	maxID := 0
	for _, todo := range todos {
		if todo.ID > maxID {
			maxID = todo.ID
		}
	}
	return maxID + 1
}

func nextSubtaskID(todo *Todo) int {
	maxID := 0
	for _, sub := range todo.Subtasks {
		if sub.ID > maxID {
			maxID = sub.ID
		}
	}
	return maxID + 1
}

func findTodo(todos []Todo, id int) (*Todo, int) {
	for i := range todos {
		if todos[i].ID == id {
			return &todos[i], i
		}
	}
	return nil, -1
}

func findSubtask(todo *Todo, id int) (*Subtask, int) {
	for i := range todo.Subtasks {
		if todo.Subtasks[i].ID == id {
			return &todo.Subtasks[i], i
		}
	}
	return nil, -1
}

func markTodoDone(todo *Todo) {
	if todo.Done {
		return
	}
	todo.Done = true
	nowTime := now().UTC()
	todo.DoneAt = &nowTime
	for i := range todo.Subtasks {
		markSubtaskDone(&todo.Subtasks[i])
	}
}

func markSubtaskDone(subtask *Subtask) {
	if subtask.Done {
		return
	}
	subtask.Done = true
	nowTime := now().UTC()
	subtask.DoneAt = &nowTime
}

func allSubtasksDone(subtasks []Subtask) bool {
	if len(subtasks) == 0 {
		return false
	}
	for _, sub := range subtasks {
		if !sub.Done {
			return false
		}
	}
	return true
}

func parseTags(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	parts := strings.Split(input, ",")
	var tags []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			tags = append(tags, part)
		}
	}
	return tags
}

func filterTodos(todos []Todo, showAll bool, priority, tag, dueBefore, filter string) ([]Todo, error) {
	var result []Todo
	var dueBeforeTime time.Time
	var err error

	if dueBefore != "" {
		dueBeforeTime, err = time.Parse("2006-01-02", dueBefore)
		if err != nil {
			return nil, fmt.Errorf("invalid due-before date: %w", err)
		}
	}

	for _, todo := range todos {
		if !showAll && todo.Done {
			continue
		}
		if priority != "" && !strings.EqualFold(strings.TrimSpace(todo.Priority), strings.TrimSpace(priority)) {
			continue
		}
		if tag != "" && !contains(todo.Tags, tag) {
			continue
		}
		if dueBefore != "" {
			if todo.DueDate == "" {
				continue
			}
			dueDate, err := time.Parse("2006-01-02", todo.DueDate)
			if err != nil {
				continue
			}
			if dueDate.After(dueBeforeTime) {
				continue
			}
		}
		if filter != "" && !matchesFilter(todo, filter) {
			continue
		}
		result = append(result, todo)
	}
	return result, nil
}

func matchesFilter(todo Todo, filter string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return true
	}
	if strings.Contains(strings.ToLower(todo.Text), filter) {
		return true
	}
	for _, tag := range todo.Tags {
		if strings.Contains(strings.ToLower(tag), filter) {
			return true
		}
	}
	for _, sub := range todo.Subtasks {
		if strings.Contains(strings.ToLower(sub.Text), filter) {
			return true
		}
	}
	return false
}

func contains(slice []string, value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	for _, item := range slice {
		if strings.ToLower(strings.TrimSpace(item)) == value {
			return true
		}
	}
	return false
}

func sortTodos(todos []Todo, sortBy string) {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "created":
		sort.SliceStable(todos, func(i, j int) bool {
			return todos[i].CreatedAt.Before(todos[j].CreatedAt)
		})
	case "due":
		sort.SliceStable(todos, func(i, j int) bool {
			return compareDueDate(todos[i].DueDate, todos[j].DueDate)
		})
	case "priority":
		sort.SliceStable(todos, func(i, j int) bool {
			return priorityValue(todos[i].Priority) < priorityValue(todos[j].Priority)
		})
	default:
		sort.SliceStable(todos, func(i, j int) bool {
			return todos[i].ID < todos[j].ID
		})
	}
}

func compareDueDate(a, b string) bool {
	if a == "" && b == "" {
		return false
	}
	if a == "" {
		return false
	}
	if b == "" {
		return true
	}
	at, errA := time.Parse("2006-01-02", a)
	bt, errB := time.Parse("2006-01-02", b)
	if errA != nil || errB != nil {
		return a < b
	}
	return at.Before(bt)
}

func priorityValue(priority string) int {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	default:
		return 4
	}
}

func printTodos(todos []Todo, showAll bool) {
	if len(todos) == 0 {
		fmt.Println("No todos found")
		return
	}

	for _, todo := range todos {
		status := "[ ]"
		if todo.Done {
			status = "[x]"
		}

		meta := []string{}
		if todo.Priority != "" {
			meta = append(meta, fmt.Sprintf("priority:%s", todo.Priority))
		}
		if todo.DueDate != "" {
			meta = append(meta, fmt.Sprintf("due:%s", todo.DueDate))
		}
		if len(todo.Tags) > 0 {
			meta = append(meta, fmt.Sprintf("tags:%s", strings.Join(todo.Tags, ",")))
		}
		info := strings.Join(meta, "; ")
		if info != "" {
			info = " (" + info + ")"
		}

		fmt.Printf("%d. %s %s%s\n", todo.ID, status, todo.Text, info)
		for _, sub := range todo.Subtasks {
			subStatus := "[ ]"
			if sub.Done {
				subStatus = "[x]"
			}
			fmt.Printf("    %d.%d %s %s\n", todo.ID, sub.ID, subStatus, sub.Text)
		}
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %s\n", err)
	os.Exit(1)
}

func runGitOutput(args ...string) (string, error) {
	gitDir, err := gitDirPath()
	if err != nil {
		return "", err
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = gitDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(output)), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func gitDirPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, todoDirName), nil
}

func gitRepoInitialized() bool {
	dir, err := gitDirPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func gitHasRemote() bool {
	output, err := runGitOutput("remote")
	return err == nil && strings.TrimSpace(output) != ""
}

func setGitRemote(remoteURL string) error {
	if gitHasRemote() {
		_, err := runGitOutput("remote", "set-url", "origin", remoteURL)
		return err
	}
	_, err := runGitOutput("remote", "add", "origin", remoteURL)
	return err
}

func commitAndPush(message string) error {
	if !gitRepoInitialized() {
		fmt.Fprintln(os.Stderr, "warning: git repository not initialized, skipping commit/push")
		return nil
	}

	if _, err := runGitOutput("add", todoFileName); err != nil {
		return err
	}

	_, err := runGitOutput("commit", "-m", message)
	if err != nil {
		if strings.Contains(err.Error(), "nothing to commit") {
			return nil
		}
		return err
	}

	if gitHasRemote() {
		if _, err := runGitOutput("push"); err != nil {
			return fmt.Errorf("git push failed: %w", err)
		}
	}
	return nil
}
