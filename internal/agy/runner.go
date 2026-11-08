package agy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

var ansiSpinnerPrefix = regexp.MustCompile(`^[\r\s⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]+.*?\r`)

// Runner manages execution of the local agy CLI binary.
type Runner struct {
	binPath               string
	defaultWorkspace      string
	defaultPermissionMode string
	uploadsDir            string
	activeCancels         sync.Map // map[string]context.CancelFunc keyed by conversation_id
}

// NewRunner creates a new CLI Runner instance.
func NewRunner(binPath, defaultWorkspace, defaultPermissionMode string) *Runner {
	return &Runner{
		binPath:               binPath,
		defaultWorkspace:      defaultWorkspace,
		defaultPermissionMode: defaultPermissionMode,
	}
}

// BuildArgs constructs the command-line flags for a ChatRequest.
func (r *Runner) BuildArgs(req ChatRequest, stream bool) []string {
	args := []string{"-p", req.Prompt}

	if stream {
		args = append(args, "--output-format", "stream-json")
	} else {
		args = append(args, "--output-format", "json")
	}

	if req.ConversationID != "" {
		args = append(args, "--conversation", req.ConversationID)
	}

	for _, dir := range req.AddDirs {
		if trimmed := strings.TrimSpace(dir); trimmed != "" {
			args = append(args, "--add-dir", trimmed)
		}
	}

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	if req.Effort != "" {
		args = append(args, "--effort", req.Effort)
	}

	if req.Agent != "" {
		args = append(args, "--agent", req.Agent)
	}

	if req.DisableSlashCommands {
		args = append(args, "--disable-slash-commands")
	}

	if req.JSONSchema != "" {
		args = append(args, "--json-schema", req.JSONSchema)
	}

	permMode := strings.TrimSpace(req.PermissionMode)
	if permMode == "" {
		permMode = r.defaultPermissionMode
	}

	switch permMode {
	case "skip-permissions", "dangerously-skip-permissions":
		args = append(args, "--dangerously-skip-permissions")
	case "auto", "accept-edits", "plan":
		args = append(args, "--mode", permMode)
	}

	return args
}

// ResolveWorkspace returns the workspace directory to run the command in.
func (r *Runner) ResolveWorkspace(requested string) string {
	if trimmed := strings.TrimSpace(requested); trimmed != "" {
		return trimmed
	}
	return r.defaultWorkspace
}

// StreamChat spawns `agy -p --output-format stream-json` and streams parsed events over a channel.
func (r *Runner) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	prepared, err := r.PrepareChatRequest(req)
	if err != nil {
		return nil, fmt.Errorf("prepare attachments: %w", err)
	}
	req = prepared

	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("prompt or attachment is required")
	}

	runCtx, cancel := context.WithCancel(ctx)
	if req.ConversationID != "" {
		r.activeCancels.Store(req.ConversationID, cancel)
	}

	args := r.BuildArgs(req, true)
	cmd := exec.CommandContext(runCtx, r.binPath, args...)
	cmd.Dir = r.ResolveWorkspace(req.WorkspaceDir)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start agy: %w", err)
	}

	events := make(chan StreamEvent, 32)

	go func() {
		defer close(events)
		defer cancel()

		var trackedConvID = req.ConversationID
		defer func() {
			if trackedConvID != "" {
				r.activeCancels.Delete(trackedConvID)
			}
		}()

		scanner := bufio.NewScanner(stdout)
		// Increase max token size to 10MB to handle large tool outputs or diffs
		scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}

			ev, err := ParseStreamLine(line)
			if err != nil {
				continue
			}

			if ev.ConversationID != "" && trackedConvID == "" {
				trackedConvID = ev.ConversationID
				r.activeCancels.Store(trackedConvID, cancel)
			}

			select {
			case events <- ev:
			case <-runCtx.Done():
				return
			}
		}

		waitErr := cmd.Wait()
		if waitErr != nil && runCtx.Err() == nil {
			errMsg := strings.TrimSpace(stderrBuf.String())
			if errMsg == "" {
				errMsg = waitErr.Error()
			}
			errPayload, _ := json.Marshal(map[string]string{
				"event": "error",
				"error": errMsg,
			})
			select {
			case events <- StreamEvent{
				Event: "error",
				Error: errMsg,
				Raw:   errPayload,
			}:
			case <-runCtx.Done():
			}
		}
	}()

	return events, nil
}

// RunChat executes a single turn and returns the final ResultPayload once complete.
func (r *Runner) RunChat(ctx context.Context, req ChatRequest) (*ResultPayload, error) {
	events, err := r.StreamChat(ctx, req)
	if err != nil {
		return nil, err
	}

	var finalResult *ResultPayload
	var lastConvID string
	var textBuilder strings.Builder

	for ev := range events {
		if ev.ConversationID != "" {
			lastConvID = ev.ConversationID
		}
		switch ev.Event {
		case "step_update":
			var su struct {
				ConversationID string `json:"conversation_id"`
				TextDelta      string `json:"text_delta"`
			}
			if err := json.Unmarshal(ev.StepUpdate, &su); err == nil {
				if su.ConversationID != "" {
					lastConvID = su.ConversationID
				}
				textBuilder.WriteString(su.TextDelta)
			}
		case "result":
			if ev.Result != nil {
				finalResult = ev.Result
			}
		case "error":
			return nil, errors.New(ev.Error)
		}
	}

	if finalResult != nil {
		if finalResult.ConversationID == "" {
			finalResult.ConversationID = lastConvID
		}
		return finalResult, nil
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return &ResultPayload{
		ConversationID: lastConvID,
		Status:         "SUCCESS",
		Response:       textBuilder.String(),
	}, nil
}

// CancelConversation cancels an actively running CLI turn by conversation ID.
func (r *Runner) CancelConversation(conversationID string) bool {
	val, ok := r.activeCancels.LoadAndDelete(conversationID)
	if !ok {
		return false
	}
	if cancel, ok := val.(context.CancelFunc); ok {
		cancel()
		return true
	}
	return false
}

// ParseStreamLine parses a single NDJSON line from `agy -p --output-format stream-json`.
func ParseStreamLine(line []byte) (StreamEvent, error) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return StreamEvent{}, errors.New("not a JSON object line")
	}

	var ev StreamEvent
	if err := json.Unmarshal(trimmed, &ev); err != nil {
		return StreamEvent{}, err
	}

	rawCopy := make([]byte, len(trimmed))
	copy(rawCopy, trimmed)
	ev.Raw = rawCopy

	if ev.ConversationID == "" && ev.Result != nil && ev.Result.ConversationID != "" {
		ev.ConversationID = ev.Result.ConversationID
	}
	if ev.ConversationID == "" && len(ev.StepUpdate) > 0 {
		var su struct {
			ConversationID string `json:"conversation_id"`
		}
		if err := json.Unmarshal(ev.StepUpdate, &su); err == nil && su.ConversationID != "" {
			ev.ConversationID = su.ConversationID
		}
	}

	return ev, nil
}

// GetUsage runs `agy -p "/usage" --output-format json` and parses the model quota buckets.
func (r *Runner) GetUsage(ctx context.Context) (*UsageData, error) {
	cmd := exec.CommandContext(ctx, r.binPath, "-p", "/usage", "--output-format", "json")
	cmd.Dir = r.defaultWorkspace
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("agy /usage failed: %w", err)
	}
	return ParseUsageJSON(out)
}

// ParseUsageJSON extracts UsageData from the JSON output of `agy -p "/usage" --output-format json`.
func ParseUsageJSON(data []byte) (*UsageData, error) {
	var res ResultPayload
	if err := json.Unmarshal(bytes.TrimSpace(data), &res); err != nil {
		return nil, fmt.Errorf("unmarshal usage result: %w", err)
	}
	if res.Command == nil || len(res.Command.Data) == 0 {
		return nil, errors.New("missing command.data in /usage response")
	}
	var usage UsageData
	if err := json.Unmarshal(res.Command.Data, &usage); err != nil {
		return nil, fmt.Errorf("unmarshal usage data: %w", err)
	}
	for gi := range usage.Groups {
		for bi := range usage.Groups[gi].Buckets {
			b := &usage.Groups[gi].Buckets[bi]
			b.RemainingPercent = math.Round(b.RemainingFraction*10000) / 100
		}
	}
	return &usage, nil
}

// ListModels runs `agy models` and parses the available models.
func (r *Runner) ListModels(ctx context.Context) ([]ModelInfo, error) {
	cmd := exec.CommandContext(ctx, r.binPath, "models")
	cmd.Dir = r.defaultWorkspace
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("agy models failed: %w", err)
	}
	return ParseModelsOutput(string(out)), nil
}

// ParseModelsOutput parses the tabular stdout of `agy models`.
func ParseModelsOutput(raw string) []ModelInfo {
	var models []ModelInfo
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		// Strip carriage-return spinner prefixes if present
		if idx := strings.LastIndex(line, "\r"); idx != -1 {
			line = line[idx+1:]
		}
		line = ansiSpinnerPrefix.ReplaceAllString(line, "")
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "Fetching available models") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		id := fields[0]
		name := strings.TrimSpace(strings.TrimPrefix(line, id))
		models = append(models, ModelInfo{
			ID:   id,
			Name: name,
		})
	}
	return models
}

// ListAgents runs `agy agents` and parses the available custom agents.
func (r *Runner) ListAgents(ctx context.Context) ([]AgentInfo, error) {
	cmd := exec.CommandContext(ctx, r.binPath, "agents")
	cmd.Dir = r.defaultWorkspace
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("agy agents failed: %w", err)
	}
	var agents []AgentInfo
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "NAME") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		desc := strings.TrimSpace(strings.TrimPrefix(line, name))
		agents = append(agents, AgentInfo{
			Name:        name,
			Description: desc,
		})
	}
	return agents, nil
}

// ListMCP runs `agy mcp list` and parses the configured MCP servers.
func (r *Runner) ListMCP(ctx context.Context) ([]MCPServerInfo, error) {
	cmd := exec.CommandContext(ctx, r.binPath, "mcp", "list")
	cmd.Dir = r.defaultWorkspace
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("agy mcp list failed: %w", err)
	}
	return ParseMCPListOutput(string(out)), nil
}

// ParseMCPListOutput parses the tabular stdout of `agy mcp list`.
func ParseMCPListOutput(raw string) []MCPServerInfo {
	var servers []MCPServerInfo
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "NAME") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		name := fields[0]
		mcpType := fields[1]
		status := fields[2]
		cmdURL := ""
		if len(fields) > 3 {
			// Find where status ends in the original line to preserve spaces in command paths
			rest := strings.TrimSpace(strings.TrimPrefix(line, name))
			rest = strings.TrimSpace(strings.TrimPrefix(rest, mcpType))
			cmdURL = strings.TrimSpace(strings.TrimPrefix(rest, status))
		}
		servers = append(servers, MCPServerInfo{
			Name:       name,
			Type:       mcpType,
			Status:     status,
			CommandURL: cmdURL,
		})
	}
	return servers
}

// CheckBinary verifies that the configured agy binary can be executed.
func (r *Runner) CheckBinary(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, r.binPath, "--help")
	return cmd.Run()
}
