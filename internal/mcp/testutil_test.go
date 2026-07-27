package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mcpSession manages a running vidscribe --mcp subprocess for protocol-level tests.
type mcpSession struct {
	cmd        *exec.Cmd
	enc        *json.Encoder
	dec        *bufio.Scanner
	nextID     int
	outputRoot string
}

// startMCPServer builds the vidscribe binary and starts it in --mcp mode.
func startMCPServer(t *testing.T) *mcpSession {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "vidscribe-test")
	if out, err := exec.Command("go", "build", "-o", bin, "../..").CombinedOutput(); err != nil {
		t.Skipf("could not build binary: %v\n%s", err, out)
	}

	outputRoot := t.TempDir()
	cmd := exec.Command(bin, "--mcp")
	cmd.Env = append(os.Environ(), "VIDSCRIBE_OUTPUT_ROOT="+outputRoot)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start mcp server: %v", err)
	}

	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})

	s := &mcpSession{
		cmd:        cmd,
		enc:        json.NewEncoder(stdin),
		dec:        bufio.NewScanner(stdout),
		outputRoot: outputRoot,
	}
	s.dec.Buffer(make([]byte, 1<<20), 1<<20)
	return s
}

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *mcpSession) send(method string, params any) {
	s.nextID++
	_ = s.enc.Encode(rpcMsg{
		JSONRPC: "2.0",
		ID:      &s.nextID,
		Method:  method,
		Params:  params,
	})
}

func (s *mcpSession) notify(method string, params any) {
	_ = s.enc.Encode(rpcMsg{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}

func (s *mcpSession) recv(t *testing.T) rpcMsg {
	return s.recvTimeout(t, 15*time.Second)
}

func (s *mcpSession) recvTimeout(t *testing.T, timeout time.Duration) rpcMsg {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.dec.Scan() {
			var msg rpcMsg
			if err := json.Unmarshal(s.dec.Bytes(), &msg); err != nil {
				t.Fatalf("unmarshal response: %v\nraw: %s", err, s.dec.Bytes())
			}
			return msg
		}
	}
	t.Fatal("timeout waiting for MCP response")
	return rpcMsg{}
}

// handshake performs the MCP initialization sequence.
func (s *mcpSession) handshake(t *testing.T) {
	t.Helper()
	s.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "vidscribe-test", "version": "1.0"},
	})
	resp := s.recv(t)
	if resp.Error != nil {
		t.Fatalf("initialize error: %v", resp.Error.Message)
	}
	s.notify("notifications/initialized", map[string]any{})
}

// callTool invokes a tool via JSON-RPC and returns the response text and isError flag.
func (s *mcpSession) callTool(t *testing.T, name string, args map[string]any) (text string, isError bool) {
	return s.callToolTimeout(t, name, args, 15*time.Second)
}

// callToolTimeout invokes a tool with a custom timeout (for long-running tools like transcribe_video).
func (s *mcpSession) callToolTimeout(t *testing.T, name string, args map[string]any, timeout time.Duration) (text string, isError bool) {
	t.Helper()
	s.send("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	resp := s.recvTimeout(t, timeout)
	if resp.Error != nil {
		t.Fatalf("tools/call RPC error: %s", resp.Error.Message)
	}
	return extractResult(t, resp.Result)
}

func (s *mcpSession) callToolWithProgress(t *testing.T, name string, args map[string]any, timeout time.Duration) (text string, isError bool, progress int) {
	t.Helper()
	s.nextID++
	id := s.nextID
	_ = s.enc.Encode(rpcMsg{JSONRPC: "2.0", ID: &id, Method: "tools/call", Params: map[string]any{
		"name": name, "arguments": args, "_meta": map[string]any{"progressToken": "e2e-progress"},
	}})
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response := s.recvTimeout(t, time.Until(deadline))
		if response.Method == "notifications/progress" {
			progress++
			continue
		}
		if response.ID != nil && *response.ID == id {
			if response.Error != nil {
				t.Fatalf("tools/call RPC error: %s", response.Error.Message)
			}
			text, isError = extractResult(t, response.Result)
			return text, isError, progress
		}
	}
	t.Fatal("timeout waiting for tool result")
	return "", true, progress
}

// extractResult parses a tool call result into text and isError.
func extractResult(t *testing.T, raw json.RawMessage) (text string, isError bool) {
	t.Helper()
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("parse call result: %v\nraw: %s", err, raw)
	}
	var sb strings.Builder
	for _, c := range result.Content {
		if c.Type == "text" {
			fmt.Fprint(&sb, c.Text)
		}
	}
	return sb.String(), result.IsError
}
