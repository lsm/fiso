package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// execCmd interface allows mocking exec.Cmd in tests
type execCmd interface {
	Run() error
	SetStdout(w *os.File)
	SetStderr(w *os.File)
	SetStdin(r *os.File)
}

// realExecCmd wraps exec.Cmd to implement the execCmd interface
type realExecCmd struct {
	cmd *exec.Cmd
}

func (r *realExecCmd) Run() error {
	return r.cmd.Run()
}

func (r *realExecCmd) SetStdout(w *os.File) {
	r.cmd.Stdout = w
}

func (r *realExecCmd) SetStderr(w *os.File) {
	r.cmd.Stderr = w
}

func (r *realExecCmd) SetStdin(r2 *os.File) {
	r.cmd.Stdin = r2
}

// execCommandFunc is the function used to execute commands.
// Tests can replace this to stub out exec.CommandContext.
var execCommandFunc = func(ctx context.Context, name string, args ...string) execCmd {
	return &realExecCmd{cmd: exec.CommandContext(ctx, name, args...)}
}

// showDockerComposeLogsFn is the function used to show docker compose logs.
// Tests can replace this to stub out the actual log display.
var showDockerComposeLogsFn = showDockerComposeLogs

// showKubernetesLogsFn is the function used to show kubernetes logs.
// Tests can replace this to stub out the actual log display.
var showKubernetesLogsFn = showKubernetesLogs

// userHomeDirFunc is the function used to get the user's home directory.
// Tests can replace this to stub out os.UserHomeDir.
var userHomeDirFunc = os.UserHomeDir

// RunLogs shows logs from fiso services.
func RunLogs(args []string) error {
	service := "fiso-flow"
	tail := 100
	follow := false

	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println(`Usage: fiso logs [flags]

Shows logs from fiso services without needing docker-compose commands.
Auto-detects if running in Docker Compose or Kubernetes.

Flags:
  --service <name>   Service name (default: fiso-flow)
  --tail <number>    Number of lines to show (default: 100)
  --follow, -f       Follow log output (like tail -f)

Examples:
  fiso logs                    Show last 100 lines of fiso-flow logs
  fiso logs --follow           Follow fiso-flow logs
  fiso logs --tail 500         Show last 500 lines
  fiso logs --service fiso-link Show logs for fiso-link`)
		return nil
	}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--service":
			if i+1 >= len(args) {
				return fmt.Errorf("--service requires a value")
			}
			service = args[i+1]
			i++
		case "--tail":
			if i+1 >= len(args) {
				return fmt.Errorf("--tail requires a value")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid tail value: %w", err)
			}
			tail = n
			i++
		case "--follow", "-f":
			follow = true
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	env, err := detectEnvironment()
	if err != nil {
		return err
	}

	switch env {
	case "docker-compose":
		return showDockerComposeLogsFn(service, tail, follow)
	case "kubernetes":
		return showKubernetesLogsFn(service, tail, follow)
	default:
		return fmt.Errorf("no supported environment found (requires docker-compose.yml or kubeconfig)")
	}
}

func detectEnvironment() (string, error) {
	if _, err := os.Stat("fiso/docker-compose.yml"); err == nil {
		if _, err := lookPathFunc("docker"); err == nil {
			return "docker-compose", nil
		}
	}

	homeDir, err := userHomeDirFunc()
	if err != nil {
		return "", err
	}

	kubeconfigPath := filepath.Join(homeDir, ".kube", "config")
	if _, err := os.Stat(kubeconfigPath); err == nil {
		if _, err := lookPathFunc("kubectl"); err == nil {
			return "kubernetes", nil
		}
	}

	return "", fmt.Errorf("no supported environment found")
}

func showDockerComposeLogs(service string, tail int, follow bool) error {
	args := buildComposeFileArgs()
	args = append(args, "logs", "--tail="+strconv.Itoa(tail))
	if follow {
		args = append(args, "-f")
	}
	args = append(args, service)

	cmd := execCommandFunc(context.Background(), "docker", args...)
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	cmd.SetStdin(os.Stdin)

	return cmd.Run()
}

func showKubernetesLogs(service string, tail int, follow bool) error {
	ctx := context.Background()
	args := []string{"logs", "--tail=" + strconv.Itoa(tail)}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, "deployment/"+service)

	cmd := execCommandFunc(ctx, "kubectl", args...)
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	cmd.SetStdin(os.Stdin)

	return cmd.Run()
}
