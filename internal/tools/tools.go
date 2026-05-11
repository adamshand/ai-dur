package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const DefaultTimeout = 10 * time.Second
const DefaultMaxBytes = 65536

var Allowed = map[string]bool{
	"pwd": true, "ls": true, "stat": true, "file": true, "wc": true, "head": true, "tail": true, "cat": true, "rg": true, "grep": true,
	"df": true, "free": true, "uptime": true, "uname": true, "id": true, "whoami": true, "hostname": true, "ps": true, "ss": true,
	"ip": true, "dig": true, "whois": true, "ping": true, "dmesg": true, "journalctl": true, "systemctl": true, "docker": true, "find": true,
}

var TrustedDirs = []string{"/bin", "/usr/bin", "/usr/local/bin", "/sbin", "/usr/sbin", "/opt/homebrew/bin", "/opt/homebrew/sbin"}

type Record struct {
	ID     int
	Trace  string
	Result string
	Denied bool
}

type Runner struct {
	Cwd     string
	NextID  int
	Records []Record
	Verbose bool
}

func NewRunner(cwd string) *Runner { return &Runner{Cwd: cwd, NextID: 1} }

func (r *Runner) Run(cmd string, args []string) Record {
	trace := strings.Join(append([]string{cmd}, args...), " ")
	result := RunReadOnly(r.Cwd, cmd, args)
	rec := Record{ID: r.NextID, Trace: trace, Result: result, Denied: strings.Contains(result, "\nstderr:\ndenied:")}
	r.NextID++
	r.Records = append(r.Records, rec)
	return rec
}

func (r *Runner) Get(id int) (Record, bool) {
	for _, rec := range r.Records {
		if rec.ID == id {
			return rec, true
		}
	}
	return Record{}, false
}
func (r *Runner) Last() (Record, bool) {
	if len(r.Records) == 0 {
		return Record{}, false
	}
	return r.Records[len(r.Records)-1], true
}

func RunReadOnly(cwd, cmd string, args []string) string {
	if len(args) > 64 {
		return format(cmd, args, 2, "", "denied: too many arguments", false)
	}
	if shellSyntax(args) {
		return format(cmd, args, 2, "", "denied: shell syntax is not available in read-only tools", false)
	}
	if cmd == "find" {
		return safeFind(cwd, args)
	}
	if !Allowed[cmd] {
		return format(cmd, args, 2, "", fmt.Sprintf("denied: command not allowed: %s", cmd), false)
	}
	safeArgs, err := validate(cmd, args, cwd)
	if err != nil {
		return format(cmd, args, 2, "", "denied: "+err.Error(), false)
	}
	bin, err := resolve(cmd)
	if err != nil {
		return format(cmd, safeArgs, 2, "", "denied: "+err.Error(), false)
	}
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout())
	defer cancel()
	ec := exec.CommandContext(ctx, bin, safeArgs...)
	ec.Dir = cwd
	ec.Stdin = nil
	ec.Env = []string{"PATH=" + strings.Join(TrustedDirs, ":"), "LANG=C", "LC_ALL=C", "TERM=dumb", "NO_COLOR=1", "RIPGREP_CONFIG_PATH=/dev/null"}
	var out, er bytes.Buffer
	ec.Stdout = &out
	ec.Stderr = &er
	runErr := ec.Run()
	code := 0
	if runErr != nil {
		if ex, ok := runErr.(*exec.ExitError); ok {
			code = ex.ExitCode()
		} else {
			code = 126
		}
	}
	if ctx.Err() == context.DeadlineExceeded {
		return format(cmd, safeArgs, 124, "", fmt.Sprintf("timed out after %s", toolTimeout()), false)
	}
	stdout, stderr := Redact(out.String()), Redact(er.String())
	max := toolMaxBytes() / 2
	stdout, ot := truncate(stdout, max)
	stderr, et := truncate(stderr, max)
	return format(cmd, safeArgs, code, stdout, stderr, ot || et)
}

func resolve(cmd string) (string, error) {
	if filepath.Base(cmd) != cmd {
		return "", errors.New("cmd must not include a path")
	}
	for _, d := range TrustedDirs {
		p := filepath.Join(d, cmd)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("command not found: %s", cmd)
}

func validate(cmd string, args []string, cwd string) ([]string, error) {
	switch cmd {
	case "cat", "head":
		return args, validatePaths(args, cwd, true, 0)
	case "tail":
		if hasFlag(args, "-f") || hasLong(args, "--follow") {
			return nil, errors.New("tail follow mode is not allowed")
		}
		return args, validatePaths(args, cwd, true, 0)
	case "stat", "file", "wc", "ls":
		return args, validatePaths(args, cwd, false, 0)
	case "rg":
		for _, a := range args {
			if a == "--pre" || strings.HasPrefix(a, "--pre=") || a == "--pre-glob" || strings.HasPrefix(a, "--pre-glob=") {
				return nil, errors.New("rg preprocessors are not allowed")
			}
			if a == "--hidden" || a == "--no-ignore" || a == "-u" || a == "-uu" || a == "-uuu" {
				return nil, errors.New("rg options that include hidden/ignored files are not allowed")
			}
		}
		return append([]string{"--no-config"}, args...), validatePaths(args, cwd, true, 1)
	case "grep":
		for _, a := range args {
			if a == "-r" || a == "-R" || a == "--recursive" || a == "--dereference-recursive" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && (strings.Contains(a, "r") || strings.Contains(a, "R"))) {
				return nil, errors.New("recursive grep is not allowed; use rg's safer defaults")
			}
		}
		return args, validatePaths(args, cwd, true, 1)
	case "journalctl":
		return validateJournalctl(args)
	case "systemctl":
		return validateSystemctl(args)
	case "docker":
		return validateDocker(args)
	case "ping":
		return validatePing(args)
	case "dmesg":
		for _, a := range args {
			if in(a, "-C", "--clear", "-c", "--read-clear", "-w", "--follow", "-W", "--follow-new") {
				return nil, fmt.Errorf("dmesg option not allowed: %s", a)
			}
		}
		return args, nil
	case "hostname":
		if len(args) > 0 {
			return nil, errors.New("hostname arguments are not allowed")
		}
		return args, nil
	case "ip":
		return validateIP(args)
	}
	return args, nil
}

func validateJournalctl(args []string) ([]string, error) {
	for _, a := range args {
		for _, p := range []string{"--follow", "-f", "--vacuum-size", "--vacuum-time", "--vacuum-files", "--rotate", "--flush", "--sync", "--update-catalog", "--setup-keys", "--new-id128", "--cursor-file", "--save-state"} {
			if a == p || strings.HasPrefix(a, p+"=") {
				return nil, fmt.Errorf("journalctl option not allowed: %s", a)
			}
		}
	}
	out := append([]string{}, args...)
	if !contains(out, "--no-pager") {
		out = append(out, "--no-pager")
	}
	if v, ok := optionValue(out, "-n", "--lines"); ok {
		if err := limitInt(v, 1000, "journalctl line count"); err != nil {
			return nil, err
		}
	} else {
		out = append(out, "-n", "200")
	}
	return out, nil
}
func validateSystemctl(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, errors.New("systemctl requires a read-only subcommand")
	}
	if !in(args[0], "status", "show", "cat", "list-units", "list-unit-files", "list-dependencies", "is-active", "is-enabled", "is-failed") {
		return nil, fmt.Errorf("systemctl subcommand not allowed: %s", args[0])
	}
	if !contains(args, "--no-pager") {
		args = append(append([]string{}, args...), "--no-pager")
	}
	return args, nil
}
func validateDocker(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, errors.New("docker requires a subcommand")
	}
	if args[0] == "ps" || (len(args) >= 2 && args[0] == "container" && args[1] == "ls") {
		return args, nil
	}
	if args[0] == "inspect" {
		if len(args) < 2 {
			return nil, errors.New("docker inspect requires an object name or id")
		}
		return args, nil
	}
	if args[0] == "logs" {
		for _, a := range args {
			if a == "-f" || a == "--follow" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "f")) {
				return nil, errors.New("docker logs follow mode is not allowed")
			}
		}
		out := append([]string{}, args...)
		if v, ok := optionValue(out, "", "--tail"); ok {
			if err := limitInt(v, 1000, "docker logs tail"); err != nil {
				return nil, err
			}
		} else {
			out = append([]string{out[0], "--tail", "200"}, out[1:]...)
		}
		return out, nil
	}
	return nil, fmt.Errorf("docker subcommand not allowed: %s", args[0])
}
func validatePing(args []string) ([]string, error) {
	for _, a := range args {
		if a == "-f" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "f")) {
			return nil, errors.New("ping flood mode is not allowed")
		}
	}
	out := append([]string{}, args...)
	if v, ok := shortValue(out, "-c"); ok {
		if err := limitInt(v, 10, "ping count"); err != nil {
			return nil, err
		}
	} else {
		out = append([]string{"-c", "4"}, out...)
	}
	if v, ok := shortValue(out, "-w"); ok {
		if err := limitInt(v, 30, "ping timeout"); err != nil {
			return nil, err
		}
	} else if v, ok := shortValue(out, "-W"); ok {
		if err := limitInt(v, 30, "ping timeout"); err != nil {
			return nil, err
		}
	} else {
		out = append([]string{"-w", "8"}, out...)
	}
	return out, nil
}
func validateIP(args []string) ([]string, error) {
	if len(args) == 0 {
		return args, nil
	}
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		if !in(args[i], "-4", "-6", "-json", "-j", "-details", "-d", "-brief", "-br", "-oneline", "-o", "-stats", "-s") {
			return nil, fmt.Errorf("ip option not allowed: %s", args[i])
		}
		i++
	}
	if i >= len(args) {
		return args, nil
	}
	if !in(args[i], "addr", "address", "route", "rule", "link", "neigh", "neighbour", "netns", "maddr") {
		return nil, fmt.Errorf("ip object not allowed: %s", args[i])
	}
	for _, a := range args[i+1:] {
		if in(a, "add", "del", "delete", "replace", "change", "set", "flush", "save", "restore", "exec", "monitor") {
			return nil, errors.New("mutating ip subcommands are not allowed")
		}
	}
	return args, nil
}

func safeFind(cwd string, args []string) string {
	roots, namePats, pathPats, typ, maxdepth, mindepth, err := parseFind(cwd, args)
	if err != nil {
		return format("find", args, 2, "", "denied: "+err.Error(), false)
	}
	var lines []string
	for _, root := range roots {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			relRoot, _ := filepath.Rel(root, path)
			depth := 0
			if relRoot != "." {
				depth = strings.Count(relRoot, string(os.PathSeparator)) + 1
			}
			if maxdepth >= 0 && depth > maxdepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if depth < mindepth {
				return nil
			}
			kind := "f"
			if d.IsDir() {
				kind = "d"
			} else if d.Type()&os.ModeSymlink != 0 {
				kind = "l"
			}
			if typ != "" && typ != kind {
				return nil
			}
			name := filepath.Base(path)
			if len(namePats) > 0 && !matchAny(name, namePats) {
				return nil
			}
			relCwd, _ := filepath.Rel(cwd, path)
			if len(pathPats) > 0 && !matchAny(relCwd, pathPats) {
				return nil
			}
			lines = append(lines, relCwd)
			if len(lines) >= 1000 {
				return filepath.SkipAll
			}
			return nil
		})
	}
	out := strings.Join(lines, "\n")
	out, tr := truncate(out, toolMaxBytes())
	return format("find", args, 0, out, "", tr)
}
func parseFind(cwd string, args []string) ([]string, []string, []string, string, int, int, error) {
	roots := []string{}
	names := []string{}
	paths := []string{}
	typ := ""
	maxd := -1
	mind := 0
	forbidden := map[string]bool{"-exec": true, "-execdir": true, "-ok": true, "-okdir": true, "-delete": true, "-fprint": true, "-fls": true, "-fprintf": true, "-printf": true}
	for i := 0; i < len(args); {
		a := args[i]
		if forbidden[a] {
			return nil, nil, nil, "", 0, 0, fmt.Errorf("find action not allowed: %s", a)
		}
		switch a {
		case "-name", "-iname", "-path", "-ipath", "-type", "-maxdepth", "-mindepth":
			if i+1 >= len(args) {
				return nil, nil, nil, "", 0, 0, fmt.Errorf("find option requires a value: %s", a)
			}
			v := args[i+1]
			switch a {
			case "-name", "-iname":
				names = append(names, v)
			case "-path", "-ipath":
				paths = append(paths, v)
			case "-type":
				if !in(v, "f", "d", "l") {
					return nil, nil, nil, "", 0, 0, errors.New("find -type supports only f, d, or l")
				}
				typ = v
			case "-maxdepth":
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 || n > 20 {
					return nil, nil, nil, "", 0, 0, errors.New("find -maxdepth must be between 0 and 20")
				}
				maxd = n
			case "-mindepth":
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 || n > 20 {
					return nil, nil, nil, "", 0, 0, errors.New("find -mindepth must be between 0 and 20")
				}
				mind = n
			}
			i += 2
		case "-print", "-and", "-a":
			i++
		case "-or", "-o", "!", "-not", "(", ")":
			return nil, nil, nil, "", 0, 0, fmt.Errorf("find expression operator not supported: %s", a)
		default:
			if strings.HasPrefix(a, "-") {
				return nil, nil, nil, "", 0, 0, fmt.Errorf("find option not supported: %s", a)
			}
			roots = append(roots, resolvePath(cwd, a))
			i++
		}
	}
	if len(roots) == 0 {
		roots = []string{cwd}
	}
	return roots, names, paths, typ, maxd, mind, nil
}

func validatePaths(args []string, cwd string, denySensitive bool, skip int) error {
	skipped := 0
	valueOpts := map[string]bool{"-n": true, "--lines": true, "-c": true, "--bytes": true, "-m": true, "--max-count": true, "-A": true, "-B": true, "-C": true, "--after-context": true, "--before-context": true, "--context": true, "--glob": true, "-g": true, "--max-depth": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			continue
		}
		if strings.HasPrefix(a, "--") && strings.Contains(a, "=") {
			continue
		}
		if valueOpts[a] {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			continue
		}
		if skipped < skip {
			skipped++
			continue
		}
		if denySensitive && looksPath(a) && sensitive(resolvePath(cwd, a)) {
			return fmt.Errorf("sensitive file path is not allowed: %s", a)
		}
	}
	return nil
}
func sensitive(path string) bool {
	n := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(n, ".pub") {
		return false
	}
	if in(n, "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "identity", ".env", ".env.local", ".env.production", ".netrc", "credentials", "credentials.json") {
		return true
	}
	return strings.HasSuffix(n, ".pem") || strings.HasSuffix(n, ".p12") || strings.HasSuffix(n, ".pfx") || strings.HasSuffix(n, ".key")
}
func resolvePath(cwd, p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, p[2:])
		}
	}
	if filepath.IsAbs(p) {
		r, _ := filepath.EvalSymlinks(p)
		if r != "" {
			return r
		}
		return filepath.Clean(p)
	}
	q := filepath.Join(cwd, p)
	r, _ := filepath.EvalSymlinks(q)
	if r != "" {
		return r
	}
	return filepath.Clean(q)
}
func looksPath(s string) bool {
	return s == "." || s == ".." || strings.Contains(s, "/") || strings.HasPrefix(s, "~")
}
func shellSyntax(args []string) bool {
	toks := map[string]bool{"|": true, "||": true, "&": true, "&&": true, "<": true, ">": true, ">>": true, "2>": true, "2>>": true, "`": true}
	for _, a := range args {
		if toks[a] || strings.ContainsRune(a, '\x00') {
			return true
		}
	}
	return false
}
func Redact(s string) string {
	pats := []*regexp.Regexp{regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`), regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)[^\s]+`), regexp.MustCompile(`(?i)((?:api[_-]?key|secret(?:[_-]?key)?|token|password|passwd|database_url|db_url|access[_-]?token|refresh[_-]?token)\s*[=:]\s*)[^\s'"\\]+`)}
	repl := []string{"[REDACTED PRIVATE KEY]", "$1[REDACTED]", "$1[REDACTED]"}
	for i, p := range pats {
		s = p.ReplaceAllString(s, repl[i])
	}
	return s
}
func format(cmd string, args []string, code int, out, er string, tr bool) string {
	suf := ""
	if tr {
		suf = "\n[output truncated]"
	}
	return fmt.Sprintf("command: %s\nexit_code: %d\nstdout:\n%s\nstderr:\n%s%s", strings.Join(append([]string{cmd}, args...), " "), code, out, er, suf)
}
func truncate(s string, n int) (string, bool) {
	if n <= 0 {
		n = 1
	}
	if len(s) <= n {
		return s, false
	}
	return s[:n], true
}
func toolTimeout() time.Duration {
	if v := os.Getenv("AIDUR_TOOL_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return DefaultTimeout
}
func toolMaxBytes() int {
	if v := os.Getenv("AIDUR_TOOL_MAX_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return DefaultMaxBytes
}
func in(s string, xs ...string) bool {
	for _, x := range xs {
		if s == x {
			return true
		}
	}
	return false
}
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func hasLong(args []string, name string) bool {
	for _, a := range args {
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, strings.TrimPrefix(flag, "-")) {
			return true
		}
	}
	return false
}
func optionValue(args []string, short, long string) (string, bool) {
	for i, a := range args {
		if short != "" && a == short && i+1 < len(args) {
			return args[i+1], true
		}
		if short != "" && strings.HasPrefix(a, short) && len(a) > len(short) {
			return a[len(short):], true
		}
		if long != "" && a == long && i+1 < len(args) {
			return args[i+1], true
		}
		if long != "" && strings.HasPrefix(a, long+"=") {
			return strings.SplitN(a, "=", 2)[1], true
		}
	}
	return "", false
}
func shortValue(args []string, flag string) (string, bool) { return optionValue(args, flag, "") }
func limitInt(v string, max int, label string) error {
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("%s must be a number no greater than %d", label, max)
	}
	if n < 0 || n > max {
		return fmt.Errorf("%s must be no greater than %d", label, max)
	}
	return nil
}
func matchAny(s string, pats []string) bool {
	sort.Strings(pats)
	for _, p := range pats {
		if ok, _ := filepath.Match(p, s); ok {
			return true
		}
	}
	return false
}
