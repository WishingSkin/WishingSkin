package runtimeplugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Calls are serialized per process. A timed-out call discards the process.
type process struct {
	mu      sync.Mutex
	retired bool
	cmd     *exec.Cmd
	input   io.WriteCloser
	output  *bufio.Scanner
	done    chan error
}

func (p *process) closeLocked() {
	if p.cmd != nil {
		p.input.Close()
		_ = p.cmd.Process.Kill()
		<-p.done
		p.cmd = nil
	}
}
func (p *process) close() { p.mu.Lock(); defer p.mu.Unlock(); p.retired = true; p.closeLocked() }
func (p *process) call(ctx context.Context, root string, pkg Package, request any) (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.retired {
		return nil, errors.New(`plugin retired`)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if p.cmd == nil {
		if err := verifyExtracted(root, pkg); err != nil {
			return nil, err
		}
		cmd := exec.Command(filepath.Join(root, filepath.FromSlash(pkg.Executables[runtime.GOOS+"/"+runtime.GOARCH])))
		cmd.Dir = root
		for _, key := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP", "LANG", "TZ"} {
			if value, ok := os.LookupEnv(key); ok {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
		}
		cmd.Env = append(cmd.Env, "WS_PLUGIN_PROTOCOL=1")
		input, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			input.Close()
			return nil, err
		}
		cmd.Stderr = io.Discard
		cmd.WaitDelay = time.Second
		if err = cmd.Start(); err != nil {
			input.Close()
			out.Close()
			return nil, err
		}
		p.cmd = cmd
		p.input = input
		p.output = bufio.NewScanner(out)
		p.output.Buffer(make([]byte, 4096), 4<<20)
		p.done = make(chan error, 1)
		go func() { p.done <- cmd.Wait() }()
	}
	type result struct {
		data json.RawMessage
		err  error
	}
	finished := make(chan result, 1)
	go func() {
		if err := json.NewEncoder(p.input).Encode(request); err != nil {
			finished <- result{err: err}
			return
		}
		if !p.output.Scan() {
			finished <- result{err: errors.New("plugin closed the response stream")}
			return
		}
		var response struct {
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		}
		if err := json.Unmarshal(p.output.Bytes(), &response); err != nil {
			finished <- result{err: err}
			return
		}
		if response.Error != "" {
			finished <- result{err: errors.New("plugin request failed")}
			return
		}
		finished <- result{data: response.Result}
	}()
	select {
	case result := <-finished:
		if result.err != nil {
			p.closeLocked()
		}
		return result.data, result.err
	case <-ctx.Done():
		p.closeLocked()
		<-finished
		return nil, ctx.Err()
	}
}
