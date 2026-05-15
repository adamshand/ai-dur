package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResultExitCode(t *testing.T) {
	if got := ResultExitCode("command: pwd\nexit_code: 7\nstdout:\n\nstderr:\n"); got != 7 {
		t.Fatalf("ResultExitCode() = %d, want 7", got)
	}
	if got := ResultExitCode("not a tool result"); got != 0 {
		t.Fatalf("ResultExitCode() = %d, want 0", got)
	}
}

func TestDeniedCommands(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  string
		args []string
		want string
	}{
		{"shell", "sh", []string{"-c", "echo hi"}, "command not allowed: sh"},
		{"interpreter", "python3", []string{"-c", "print('hi')"}, "command not allowed: python3"},
		{"mutating", "touch", []string{"/tmp/x"}, "command not allowed: touch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RunReadOnly(t.TempDir(), tc.cmd, tc.args)
			if !strings.Contains(got, "denied: "+tc.want) {
				t.Fatalf("got\n%s\nwant %q", got, tc.want)
			}
		})
	}
}

func TestShellSyntaxDenied(t *testing.T) {
	for _, args := range [][]string{{"hello", ">", "out"}, {"hello", "|", "wc"}, {"hello", "&&", "echo"}} {
		got := RunReadOnly(t.TempDir(), "cat", args)
		if !strings.Contains(got, "shell syntax is not available") {
			t.Fatalf("args=%v got\n%s", args, got)
		}
	}
}

func TestTailFollowDenied(t *testing.T) {
	got := RunReadOnly(t.TempDir(), "tail", []string{"-f", "log"})
	if !strings.Contains(got, "tail follow mode is not allowed") {
		t.Fatal(got)
	}
}

func TestRgDangerousOptionsDenied(t *testing.T) {
	for _, args := range [][]string{{"--pre", "cat", "x"}, {"--pre=cat", "x"}, {"--hidden", "x"}, {"--no-ignore", "x"}, {"-u", "x"}} {
		got := RunReadOnly(t.TempDir(), "rg", args)
		if !strings.Contains(got, "denied:") {
			t.Fatalf("args=%v got\n%s", args, got)
		}
	}
}

func TestRgInjectsNoConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := RunReadOnly(dir, "rg", []string{"hello", "hello.txt"})
	if !strings.Contains(got, "command: rg --no-config hello hello.txt") {
		t.Fatal(got)
	}
}

func TestGrepRecursiveDenied(t *testing.T) {
	for _, args := range [][]string{{"-r", "x", "."}, {"-R", "x", "."}, {"--recursive", "x", "."}} {
		got := RunReadOnly(t.TempDir(), "grep", args)
		if !strings.Contains(got, "recursive grep is not allowed") {
			t.Fatalf("args=%v got\n%s", args, got)
		}
	}
}

func TestJournalctlValidators(t *testing.T) {
	if got := RunReadOnly(t.TempDir(), "journalctl", []string{"--follow"}); !strings.Contains(got, "journalctl option not allowed") {
		t.Fatal(got)
	}
	if got := RunReadOnly(t.TempDir(), "journalctl", []string{"-n", "all"}); !strings.Contains(got, "journalctl line count") {
		t.Fatal(got)
	}
}

func TestSystemctlValidators(t *testing.T) {
	if got := RunReadOnly(t.TempDir(), "systemctl", []string{"restart", "nginx"}); !strings.Contains(got, "systemctl subcommand not allowed: restart") {
		t.Fatal(got)
	}
	args, err := validateSystemctl([]string{"list-units", "--failed", "--no-pager"})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	want := "list-units --failed --no-pager"
	if got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestDockerValidators(t *testing.T) {
	if got := RunReadOnly(t.TempDir(), "docker", []string{"run", "alpine"}); !strings.Contains(got, "docker subcommand not allowed: run") {
		t.Fatal(got)
	}
	if got := RunReadOnly(t.TempDir(), "docker", []string{"logs", "-f", "web"}); !strings.Contains(got, "docker logs follow mode is not allowed") {
		t.Fatal(got)
	}
	if got := RunReadOnly(t.TempDir(), "docker", []string{"logs", "--tail", "all", "web"}); !strings.Contains(got, "docker logs tail must be a number") {
		t.Fatal(got)
	}
}

func TestPingValidators(t *testing.T) {
	if got := RunReadOnly(t.TempDir(), "ping", []string{"-f", "example.com"}); !strings.Contains(got, "ping flood mode is not allowed") {
		t.Fatal(got)
	}
	if got := RunReadOnly(t.TempDir(), "ping", []string{"-c", "999", "example.com"}); !strings.Contains(got, "ping count must be no greater than 10") {
		t.Fatal(got)
	}
}

func TestDmesgHostnameIPValidators(t *testing.T) {
	if got := RunReadOnly(t.TempDir(), "dmesg", []string{"--clear"}); !strings.Contains(got, "dmesg option not allowed") {
		t.Fatal(got)
	}
	if got := RunReadOnly(t.TempDir(), "hostname", []string{"new-host"}); !strings.Contains(got, "hostname arguments are not allowed") {
		t.Fatal(got)
	}
	if got := RunReadOnly(t.TempDir(), "ip", []string{"link", "set", "eth0", "down"}); !strings.Contains(got, "ip link subcommand not allowed: set") {
		t.Fatal(got)
	}
}

func TestIPValidatorAllowsOnlyReadOnlySubcommands(t *testing.T) {
	allowed := [][]string{
		{"addr"},
		{"addr", "show"},
		{"route", "get", "192.0.2.1"},
		{"netns", "pids", "example"},
	}
	for _, args := range allowed {
		if _, err := validateIP(args); err != nil {
			t.Fatalf("validateIP(%v) denied read-only command: %v", args, err)
		}
	}

	denied := [][]string{
		{"route", "append", "default", "via", "192.0.2.1"},
		{"netns", "attach", "example", "1234"},
		{"link", "set", "eth0", "down"},
		{"addr", "add", "192.0.2.2/24", "dev", "eth0"},
	}
	for _, args := range denied {
		if _, err := validateIP(args); err == nil {
			t.Fatalf("validateIP(%v) allowed mutating command", args)
		}
	}
}

func TestSensitiveFileDeniedForContentReaders(t *testing.T) {
	dir := t.TempDir()
	ssh := filepath.Join(dir, ".ssh")
	if err := os.Mkdir(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ssh, "id_rsa"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ssh, "id_rsa.pub"), []byte("public"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("TOKEN=secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := RunReadOnly(dir, "cat", []string{".env"}); !strings.Contains(got, "sensitive file path is not allowed") {
		t.Fatal(got)
	}
	if got := RunReadOnly(dir, "cat", []string{".ssh/id_rsa"}); !strings.Contains(got, "sensitive file path is not allowed") {
		t.Fatal(got)
	}
	if got := RunReadOnly(dir, "cat", []string{"-n", ".ssh/id_rsa"}); !strings.Contains(got, "sensitive file path is not allowed") {
		t.Fatal(got)
	}
	if got := RunReadOnly(dir, "grep", []string{"-n", "secret", ".ssh/id_rsa"}); !strings.Contains(got, "sensitive file path is not allowed") {
		t.Fatal(got)
	}
	if got := RunReadOnly(dir, "rg", []string{"-n", "secret", ".ssh/id_rsa"}); !strings.Contains(got, "sensitive file path is not allowed") {
		t.Fatal(got)
	}
	if got := RunReadOnly(dir, "cat", []string{".ssh/id_rsa.pub"}); !strings.Contains(got, "public") {
		t.Fatal(got)
	}
}

func TestToolsCanReadOutsideCwd(t *testing.T) {
	cwd := t.TempDir()
	outside := t.TempDir()
	path := filepath.Join(outside, "hostname")
	if err := os.WriteFile(path, []byte("prod-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := RunReadOnly(cwd, "cat", []string{path})
	if !strings.Contains(got, "prod-1") {
		t.Fatal(got)
	}
}

func TestSafeFindFindsFilesAndDeniesMutatingActions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.py"), []byte("print('hi')\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := RunReadOnly(t.TempDir(), "find", []string{dir, "-maxdepth", "1", "-type", "f", "-name", "*.py"})
	if !strings.Contains(got, "app.py") {
		t.Fatal(got)
	}
	for _, args := range [][]string{{dir, "-delete"}, {dir, "-exec", "id", ";"}, {dir, "-fprint", "/tmp/out"}} {
		got := RunReadOnly(t.TempDir(), "find", args)
		if !strings.Contains(got, "find action not allowed") {
			t.Fatalf("args=%v got\n%s", args, got)
		}
	}
}

func TestRedactSecrets(t *testing.T) {
	input := "API_KEY=abc123\nAuthorization: Bearer token123\n-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----"
	got := Redact(input)
	for _, secret := range []string{"abc123", "token123", "OPENSSH PRIVATE KEY"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret %q leaked in %q", secret, got)
		}
	}
}
