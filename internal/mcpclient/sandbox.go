package mcpclient

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Runner starts a subprocess with the given command, args, and env, and
// returns a Process wrapping its standard streams. Tests inject a FakeRunner;
// production uses defaultRunner backed by os/exec.
type Runner interface {
	Start(cmd string, args []string, env []string) (Process, error)
}

// Process is the subset of an os/exec.Cmd surface mcpclient needs. Stdin,
// Stdout, and Stderr return the long-lived pipes; Kill terminates the
// subprocess; Wait blocks until it exits.
type Process interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Kill() error
	Wait() error
}

// defaultRunner is the production Runner that shells out via os/exec.
type defaultRunner struct{}

// DefaultRunner returns the production stdio Runner.
func DefaultRunner() Runner { return defaultRunner{} }

func (defaultRunner) Start(cmd string, args []string, env []string) (Process, error) {
	c := exec.Command(cmd, args...)
	c.Env = env
	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := c.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start: %w", err)
	}
	return &execProcess{cmd: c, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

type execProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (p *execProcess) Stdin() io.WriteCloser  { return p.stdin }
func (p *execProcess) Stdout() io.ReadCloser  { return p.stdout }
func (p *execProcess) Stderr() io.ReadCloser  { return p.stderr }
func (p *execProcess) Wait() error            { return p.cmd.Wait() }
func (p *execProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

// buildEnv returns a restricted env slice for the subprocess. Only PATH, HOME,
// and the explicitly-declared keys in extra are propagated. PATH and HOME are
// inherited from the current process when present; anything else from the
// host shell is dropped. Keys in extra always win over the inherited PATH/HOME.
func buildEnv(extra map[string]string) []string {
	out := []string{}
	if v, ok := os.LookupEnv("PATH"); ok {
		out = append(out, "PATH="+v)
	}
	if v, ok := os.LookupEnv("HOME"); ok {
		out = append(out, "HOME="+v)
	}
	// Override or append explicit keys. Iterate deterministically: any caller
	// who needs stable env ordering for testing should construct extra so the
	// keys do not collide with PATH/HOME, and inspect the slice after.
	for k, v := range extra {
		// Replace PATH/HOME if explicitly set by caller.
		replaced := false
		for i, existing := range out {
			if len(existing) > len(k)+1 && existing[:len(k)+1] == k+"=" {
				out[i] = k + "=" + v
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, k+"="+v)
		}
	}
	return out
}
