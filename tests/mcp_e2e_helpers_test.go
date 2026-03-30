//go:build e2e

package tests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const mcpResponseTimeout = 5 * time.Second

var mcpBinaryPath string

type mcpE2EServer struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr bytes.Buffer
	nextID int
}

type mcpRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "amm-mcp-e2e-*")
	if err != nil {
		panic(err)
	}

	mcpBinaryPath = filepath.Join(tmpDir, "amm-mcp")

	goBin, err := exec.LookPath("go")
	if err != nil {
		fallback := "/usr/local/go/bin/go"
		if _, statErr := os.Stat(fallback); statErr == nil {
			goBin = fallback
		} else {
			_ = os.RemoveAll(tmpDir)
			panic("go binary not found in PATH and no fallback at /usr/local/go/bin/go")
		}
	}

	repoRoot := detectRepoRoot()
	cmd := exec.Command(goBin, "build", "-tags", "fts5", "-o", mcpBinaryPath, "./cmd/amm-mcp")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(tmpDir)
		panic("build amm-mcp failed: " + string(out) + ": " + err.Error())
	}

	code := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

func detectRepoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}

	candidates := []string{wd, filepath.Dir(wd)}
	for _, candidate := range candidates {
		if _, statErr := os.Stat(filepath.Join(candidate, "cmd", "amm-mcp", "main.go")); statErr == nil {
			return candidate
		}
	}
	return wd
}

func startMCPServer(t *testing.T) *mcpE2EServer {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "mcp-e2e.db")
	cmd := exec.Command(mcpBinaryPath)
	cmd.Env = append(os.Environ(), "AMM_DB_PATH="+dbPath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}

	srv := &mcpE2EServer{
		t:      t,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdoutPipe),
		nextID: 1,
	}
	cmd.Stderr = &srv.stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start amm-mcp: %v", err)
	}
	t.Cleanup(func() { srv.Close() })

	initResp, err := srv.SendRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo": map[string]string{
			"name":    "amm-mcp-e2e",
			"version": "1.0.0",
		},
		"capabilities": map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("initialize request failed: %v", err)
	}
	if initResp.Error != nil {
		t.Fatalf("initialize RPC error: %+v", initResp.Error)
	}

	if err := srv.sendNotification("initialized", map[string]interface{}{}); err != nil {
		t.Fatalf("initialized notification failed: %v", err)
	}

	resp, ok, err := srv.readResponseWithin(500 * time.Millisecond)
	if err != nil {
		t.Fatalf("reading post-initialized response failed: %v", err)
	}
	if ok && resp.Error != nil && resp.Error.Code != -32601 {
		t.Fatalf("unexpected initialized handling response: %+v", resp)
	}

	return srv
}

func (s *mcpE2EServer) SendRequest(method string, params interface{}) (mcpResponse, error) {
	id := fmt.Sprintf("%d", s.nextID)
	s.nextID++

	req := mcpRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := s.writeRequest(req); err != nil {
		return mcpResponse{}, err
	}

	for {
		resp, err := s.readResponse(mcpResponseTimeout)
		if err != nil {
			return mcpResponse{}, err
		}
		if resp.ID == id {
			return resp, nil
		}
	}
}

func (s *mcpE2EServer) sendNotification(method string, params interface{}) error {
	req := mcpRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return s.writeRequest(req)
}

func (s *mcpE2EServer) writeRequest(req mcpRequest) error {
	line, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	line = append(line, '\n')
	if _, err := s.stdin.Write(line); err != nil {
		return fmt.Errorf("write request: %w", err)
	}
	return nil
}

func (s *mcpE2EServer) readResponse(timeout time.Duration) (mcpResponse, error) {
	resp, ok, err := s.readResponseWithin(timeout)
	if err != nil {
		return mcpResponse{}, err
	}
	if !ok {
		return mcpResponse{}, fmt.Errorf("timeout waiting for response")
	}
	return resp, nil
}

func (s *mcpE2EServer) readResponseWithin(timeout time.Duration) (mcpResponse, bool, error) {
	type readResult struct {
		resp mcpResponse
		err  error
	}

	ch := make(chan readResult, 1)
	go func() {
		line, err := s.stdout.ReadBytes('\n')
		if err != nil {
			ch <- readResult{err: err}
			return
		}
		var resp mcpResponse
		if err := json.Unmarshal(bytes.TrimSpace(line), &resp); err != nil {
			ch <- readResult{err: fmt.Errorf("decode response %q: %w", string(line), err)}
			return
		}
		ch <- readResult{resp: resp}
	}()

	select {
	case result := <-ch:
		if result.err != nil {
			return mcpResponse{}, false, result.err
		}
		return result.resp, true, nil
	case <-time.After(timeout):
		return mcpResponse{}, false, nil
	}
}

func (s *mcpE2EServer) Close() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}

	_ = s.stdin.Close()

	done := make(chan error, 1)
	go func() {
		done <- s.cmd.Wait()
	}()

	select {
	case <-time.After(2 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	case <-done:
	}

	if s.t != nil && s.t.Failed() && s.stderr.Len() > 0 {
		s.t.Logf("amm-mcp stderr:\n%s", s.stderr.String())
	}

	s.cmd = nil
}

func sendToolCall(server *mcpE2EServer, toolName string, args map[string]interface{}) (json.RawMessage, error) {
	resp, err := server.SendRequest("tools/call", map[string]interface{}{
		"name":      toolName,
		"arguments": args,
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error (%d): %s", resp.Error.Code, resp.Error.Message)
	}

	var payload struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &payload); err != nil {
		return nil, fmt.Errorf("decode tool payload: %w", err)
	}
	if len(payload.Content) == 0 {
		return nil, fmt.Errorf("missing tool payload content")
	}
	if payload.IsError {
		return nil, fmt.Errorf("tool returned error content: %s", payload.Content[0].Text)
	}

	return json.RawMessage(payload.Content[0].Text), nil
}
