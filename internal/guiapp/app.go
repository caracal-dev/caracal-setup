package guiapp

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var (
	usernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	hostnamePattern = regexp.MustCompile(
		`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?` +
			`(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`,
	)
)

type App struct {
	ctx context.Context

	mu      sync.Mutex
	running bool
}

type ProfileView struct {
	CurrentUsername string `json:"currentUsername"`
	CurrentHome     string `json:"currentHome"`
	CurrentHostname string `json:"currentHostname"`
}

type SetupRequest struct {
	ChangeAccount  bool   `json:"changeAccount"`
	ChangeHostname bool   `json:"changeHostname"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	Hostname       string `json:"hostname"`
}

type SetupResult struct {
	AppliedUsername string `json:"appliedUsername"`
	AppliedHome     string `json:"appliedHome"`
	AppliedHostname string `json:"appliedHostname"`
	RebootRequired  bool   `json:"rebootRequired"`
}

type PhaseEvent struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Message string `json:"message"`
}

type terminalCandidate struct {
	Command string
	Args    func(home string, script string) []string
}

func New() *App {
	return &App{}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) GetProfile() ProfileView {
	current := currentDesktopUser()
	return ProfileView{
		CurrentUsername: current,
		CurrentHome:     homeDirForUser(current),
		CurrentHostname: currentHostname(),
	}
}

func (a *App) RunSetup(request SetupRequest) (SetupResult, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return SetupResult{}, fmt.Errorf("setup is already running")
	}
	a.running = true
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
	}()

	currentUser := currentDesktopUser()
	if currentUser == "" {
		return SetupResult{}, fmt.Errorf("could not determine the current desktop user")
	}

	result, changed, err := a.applyDetails(currentUser, request, true)
	if err != nil {
		return SetupResult{}, err
	}

	targetUser := result.AppliedUsername
	targetHome := homeDirForUser(targetUser)
	if targetHome == "" {
		targetHome = filepath.Join("/home", targetUser)
	}
	result.AppliedHome = targetHome

	a.emitPhase("first-run", "Mandatory Setup", "running", "Opening a terminal to run ujust first-run...")
	if err := runFirstRunInTerminal(targetUser, targetHome); err != nil {
		a.emitPhase("first-run", "Mandatory Setup", "error", err.Error())
		return SetupResult{}, err
	}
	a.emitPhase("first-run", "Mandatory Setup", "complete", "ujust first-run finished successfully.")
	a.emitPhase("finish", "Reboot", "ready", "Setup is complete. Reboot now to finish applying group and session changes.")

	result.RebootRequired = true
	if !changed {
		result.AppliedHostname = currentHostname()
	}
	return result, nil
}

func (a *App) SaveDetails(request SetupRequest) (SetupResult, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return SetupResult{}, fmt.Errorf("setup is already running")
	}
	a.running = true
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
	}()

	currentUser := currentDesktopUser()
	if currentUser == "" {
		return SetupResult{}, fmt.Errorf("could not determine the current desktop user")
	}

	result, changed, err := a.applyDetails(currentUser, request, false)
	if err != nil {
		return SetupResult{}, err
	}
	if changed {
		a.emitPhase("finish", "Saved", "ready", "Details saved. Reboot or sign out to apply the new session state.")
	} else {
		a.emitPhase("finish", "Saved", "ready", "No account or hostname changes were requested.")
	}
	return result, nil
}

func (a *App) RebootNow() error {
	a.emitPhase("finish", "Reboot", "running", "Requesting a system reboot...")
	if err := runPrivilegedCommand(nil, "systemctl", "reboot"); err != nil {
		a.emitPhase("finish", "Reboot", "error", err.Error())
		return err
	}
	return nil
}

func (a *App) emitPhase(id string, title string, state string, message string) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "setup:phase", PhaseEvent{
		ID:      id,
		Title:   title,
		State:   state,
		Message: message,
	})
}

func (a *App) applyDetails(currentUser string, request SetupRequest, allowNoChanges bool) (SetupResult, bool, error) {
	targetUser, targetHostname, changed, err := validateSetupRequest(currentUser, request, allowNoChanges)
	if err != nil {
		a.emitPhase("account", "Details", "error", err.Error())
		return SetupResult{}, false, err
	}

	result := SetupResult{
		AppliedUsername: targetUser,
		AppliedHome:     homeDirForUser(targetUser),
		AppliedHostname: targetHostname,
		RebootRequired:  changed,
	}

	if changed {
		a.emitPhase("account", "Details", "running", "Saving your account and hostname changes...")
		scriptHostname := ""
		if request.ChangeHostname {
			scriptHostname = targetHostname
		}
		if err := runAccountUpdate(currentUser, targetUser, scriptHostname, request.Password); err != nil {
			a.emitPhase("account", "Details", "error", err.Error())
			return SetupResult{}, false, err
		}
		a.emitPhase("account", "Details", "complete", "Details saved.")
	} else {
		a.emitPhase("account", "Details", "complete", "Keeping the current account and hostname.")
	}

	return result, changed, nil
}

func validateSetupRequest(currentUser string, request SetupRequest, allowNoChanges bool) (string, string, bool, error) {
	username := strings.TrimSpace(request.Username)
	if username == "" {
		username = currentUser
	}

	currentHost := currentHostname()
	hostname := strings.TrimSpace(request.Hostname)
	if hostname == "" {
		hostname = currentHost
	}

	switch {
	case !usernamePattern.MatchString(username):
		return "", "", false, fmt.Errorf("use a lowercase username starting with a letter or underscore")
	case strings.ContainsRune(request.Password, '\n'):
		return "", "", false, fmt.Errorf("password cannot contain newlines")
	case request.ChangeHostname && !hostnamePattern.MatchString(hostname):
		return "", "", false, fmt.Errorf("use a valid hostname")
	}

	if username == currentUser {
		request.ChangeAccount = request.ChangeAccount && request.Password != ""
	} else if _, err := user.Lookup(username); err == nil {
		return "", "", false, fmt.Errorf("the username %q already exists", username)
	}

	passwordChanged := request.Password != ""
	usernameChanged := username != currentUser
	hostnameChanged := request.ChangeHostname && hostname != currentHost
	changed := usernameChanged || passwordChanged || hostnameChanged
	if !changed && !allowNoChanges {
		return "", "", false, fmt.Errorf("change the username, password, or hostname before saving")
	}

	return username, hostname, changed, nil
}

func runAccountUpdate(currentUser string, targetUser string, targetHostname string, password string) error {
	script, err := resolveScriptPath("apply-account-settings.sh")
	if err != nil {
		return err
	}

	stdin := bytes.NewBufferString(password + "\n")
	if err := runPrivilegedCommand(stdin, script, currentUser, targetUser, targetHostname); err != nil {
		return fmt.Errorf("could not save details: %w", err)
	}
	return nil
}

func runFirstRunInTerminal(targetUser string, targetHome string) error {
	terminal, err := findTerminal()
	if err != nil {
		return err
	}

	script := strings.Join([]string{
		`printf '\nCaracal Setup\n==============\n\n'`,
		`echo 'The mandatory first-run setup is starting now.'`,
		`echo 'Finish any prompts in this terminal window.'`,
		`echo`,
		`ujust first-run`,
		`status=$?`,
		`echo`,
		`if [[ $status -eq 0 ]]; then`,
		`  echo 'First-run setup finished successfully.'`,
		`else`,
		`  echo "First-run setup failed with exit code $status."`,
		`fi`,
		`echo`,
		`read -r -n 1 -s -p 'Press any key to return to Caracal Setup...'`,
		`echo`,
		`exit $status`,
	}, "\n")

	args := append([]string(nil), terminal.Args(targetHome, script)...)
	cmd := exec.Command(terminal.Command, args...)
	cmd.Env = withUserEnvironment(os.Environ(), targetUser, targetHome)
	cmd.Dir = targetHome

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ujust first-run did not complete successfully: %w", err)
	}

	return nil
}

func withUserEnvironment(base []string, username string, home string) []string {
	filtered := make([]string, 0, len(base)+5)
	for _, item := range base {
		if strings.HasPrefix(item, "HOME=") ||
			strings.HasPrefix(item, "USER=") ||
			strings.HasPrefix(item, "LOGNAME=") ||
			strings.HasPrefix(item, "SUDO_USER=") ||
			strings.HasPrefix(item, "PWD=") {
			continue
		}
		filtered = append(filtered, item)
	}

	filtered = append(filtered,
		"HOME="+home,
		"USER="+username,
		"LOGNAME="+username,
		"SUDO_USER="+username,
		"PWD="+home,
	)
	return filtered
}

func runPrivilegedCommand(stdin *bytes.Buffer, args ...string) error {
	pkexecPath, err := exec.LookPath("pkexec")
	if err != nil {
		return fmt.Errorf("pkexec is not installed")
	}
	envPath, err := exec.LookPath("env")
	if err != nil {
		return fmt.Errorf("could not locate env for pkexec wrapper: %w", err)
	}

	cmdArgs := append([]string{envPath}, args...)
	cmd := exec.Command(pkexecPath, cmdArgs...)
	if stdin != nil {
		cmd.Stdin = stdin
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return err
		}
		return fmt.Errorf("%v: %s", err, trimmed)
	}

	return nil
}

func resolveScriptPath(name string) (string, error) {
	if envDir := strings.TrimSpace(os.Getenv("CARACAL_SETUP_SCRIPT_DIR")); envDir != "" {
		candidate := filepath.Join(envDir, name)
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}

	var candidates []string
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, candidateRelativePaths(wd, filepath.Join("scripts", name))...)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, candidateRelativePaths(filepath.Dir(exe), filepath.Join("scripts", name))...)
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "..", "lib", "caracal-setup", "scripts", name))
	}
	candidates = append(candidates, filepath.Join("/usr/lib/caracal-setup/scripts", name))

	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		clean := filepath.Clean(candidate)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		if isExecutableFile(clean) {
			return clean, nil
		}
	}

	return "", fmt.Errorf("could not locate %s", name)
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

func findTerminal() (terminalCandidate, error) {
	candidates := preferredTerminalCandidates()
	candidates = append(candidates, []terminalCandidate{
		{
			Command: "alacritty",
			Args: func(home string, script string) []string {
				return []string{"--working-directory", home, "-T", "Caracal Setup", "-e", "bash", "-lc", script}
			},
		},
		{
			Command: "gnome-terminal",
			Args: func(home string, script string) []string {
				return []string{"--working-directory", home, "--", "bash", "-lc", script}
			},
		},
		{
			Command: "konsole",
			Args: func(home string, script string) []string {
				return []string{"--workdir", home, "-e", "bash", "-lc", script}
			},
		},
		{
			Command: "ptyxis",
			Args: func(home string, script string) []string {
				return []string{"--working-directory", home, "--", "bash", "-lc", script}
			},
		},
		{
			Command: "kgx",
			Args: func(home string, script string) []string {
				return []string{"--working-directory", home, "bash", "-lc", script}
			},
		},
		{
			Command: "kitty",
			Args: func(home string, script string) []string {
				return []string{"--directory", home, "bash", "-lc", script}
			},
		},
		{
			Command: "wezterm",
			Args: func(home string, script string) []string {
				return []string{"start", "--cwd", home, "--", "bash", "-lc", script}
			},
		},
		{
			Command: "xfce4-terminal",
			Args: func(home string, script string) []string {
				return []string{"--working-directory=" + home, "--command", "bash -lc " + shellQuote(script)}
			},
		},
		{
			Command: "mate-terminal",
			Args: func(home string, script string) []string {
				return []string{"--working-directory", home, "--", "bash", "-lc", script}
			},
		},
		{
			Command: "lxterminal",
			Args: func(home string, script string) []string {
				return []string{"--working-directory=" + home, "-e", "bash -lc " + shellQuote(script)}
			},
		},
		{
			Command: "x-terminal-emulator",
			Args: func(home string, script string) []string {
				return []string{"-e", "bash", "-lc", script}
			},
		},
		{
			Command: "xterm",
			Args: func(home string, script string) []string {
				return []string{"-T", "Caracal Setup", "-e", "bash", "-lc", script}
			},
		},
	}...)

	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		if _, ok := seen[candidate.Command]; ok {
			continue
		}
		seen[candidate.Command] = struct{}{}
		if _, err := exec.LookPath(candidate.Command); err == nil {
			return candidate, nil
		}
	}

	return terminalCandidate{}, fmt.Errorf("no supported terminal emulator was found to run ujust first-run; install a desktop terminal such as Alacritty, Konsole, or GNOME Terminal")
}

func preferredTerminalCandidates() []terminalCandidate {
	var candidates []terminalCandidate

	if value := strings.TrimSpace(os.Getenv("TERMINAL")); value != "" {
		if candidate, ok := commandToTerminalCandidate(value); ok {
			candidates = append(candidates, candidate)
		}
	}

	if value := strings.TrimSpace(readKDETerminalApplication()); value != "" {
		if candidate, ok := desktopIDToTerminalCandidate(value); ok {
			candidates = append(candidates, candidate)
		}
	}

	return candidates
}

func readKDETerminalApplication() string {
	if _, err := exec.LookPath("kreadconfig6"); err != nil {
		return ""
	}

	out, err := exec.Command("kreadconfig6", "--file", "kdeglobals", "--group", "General", "--key", "TerminalApplication").Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

func desktopIDToTerminalCandidate(id string) (terminalCandidate, bool) {
	normalized := strings.TrimSpace(id)
	switch normalized {
	case "org.alacritty.Alacritty.desktop", "Alacritty.desktop":
		return commandToTerminalCandidate("alacritty")
	case "org.kde.konsole.desktop":
		return commandToTerminalCandidate("konsole")
	case "org.gnome.Console.desktop":
		return commandToTerminalCandidate("kgx")
	case "org.gnome.Terminal.desktop":
		return commandToTerminalCandidate("gnome-terminal")
	case "org.wezfurlong.wezterm.desktop":
		return commandToTerminalCandidate("wezterm")
	case "kitty.desktop":
		return commandToTerminalCandidate("kitty")
	}

	execLine, err := readDesktopExec(normalized)
	if err != nil {
		return terminalCandidate{}, false
	}
	fields := strings.Fields(execLine)
	if len(fields) == 0 {
		return terminalCandidate{}, false
	}
	return commandToTerminalCandidate(fields[0])
}

func readDesktopExec(id string) (string, error) {
	for _, dir := range desktopApplicationDirs() {
		path := filepath.Join(dir, id)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "Exec=") {
				return strings.TrimSpace(strings.TrimPrefix(line, "Exec=")), nil
			}
		}
		return "", fs.ErrNotExist
	}

	return "", fs.ErrNotExist
}

func desktopApplicationDirs() []string {
	dirs := []string{
		filepath.Join(os.Getenv("HOME"), ".local", "share", "applications"),
		"/usr/local/share/applications",
		"/usr/share/applications",
	}
	return dirs
}

func commandToTerminalCandidate(command string) (terminalCandidate, bool) {
	switch filepath.Base(command) {
	case "alacritty":
		return terminalCandidate{
			Command: "alacritty",
			Args: func(home string, script string) []string {
				return []string{"--working-directory", home, "-T", "Caracal Setup", "-e", "bash", "-lc", script}
			},
		}, true
	case "gnome-terminal":
		return terminalCandidate{
			Command: "gnome-terminal",
			Args: func(home string, script string) []string {
				return []string{"--working-directory", home, "--", "bash", "-lc", script}
			},
		}, true
	case "konsole":
		return terminalCandidate{
			Command: "konsole",
			Args: func(home string, script string) []string {
				return []string{"--workdir", home, "-e", "bash", "-lc", script}
			},
		}, true
	case "ptyxis":
		return terminalCandidate{
			Command: "ptyxis",
			Args: func(home string, script string) []string {
				return []string{"--working-directory", home, "--", "bash", "-lc", script}
			},
		}, true
	case "kgx":
		return terminalCandidate{
			Command: "kgx",
			Args: func(home string, script string) []string {
				return []string{"--working-directory", home, "bash", "-lc", script}
			},
		}, true
	case "kitty":
		return terminalCandidate{
			Command: "kitty",
			Args: func(home string, script string) []string {
				return []string{"--directory", home, "bash", "-lc", script}
			},
		}, true
	case "wezterm":
		return terminalCandidate{
			Command: "wezterm",
			Args: func(home string, script string) []string {
				return []string{"start", "--cwd", home, "--", "bash", "-lc", script}
			},
		}, true
	case "xfce4-terminal":
		return terminalCandidate{
			Command: "xfce4-terminal",
			Args: func(home string, script string) []string {
				return []string{"--working-directory=" + home, "--command", "bash -lc " + shellQuote(script)}
			},
		}, true
	case "mate-terminal":
		return terminalCandidate{
			Command: "mate-terminal",
			Args: func(home string, script string) []string {
				return []string{"--working-directory", home, "--", "bash", "-lc", script}
			},
		}, true
	case "lxterminal":
		return terminalCandidate{
			Command: "lxterminal",
			Args: func(home string, script string) []string {
				return []string{"--working-directory=" + home, "-e", "bash -lc " + shellQuote(script)}
			},
		}, true
	case "x-terminal-emulator":
		return terminalCandidate{
			Command: "x-terminal-emulator",
			Args: func(home string, script string) []string {
				return []string{"-e", "bash", "-lc", script}
			},
		}, true
	case "xterm":
		return terminalCandidate{
			Command: "xterm",
			Args: func(home string, script string) []string {
				return []string{"-T", "Caracal Setup", "-e", "bash", "-lc", script}
			},
		}, true
	}

	return terminalCandidate{}, false
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func homeDirForUser(username string) string {
	if username == "" {
		if value, err := os.UserHomeDir(); err == nil {
			return value
		}
		return ""
	}

	if lookedUp, err := user.Lookup(username); err == nil && strings.TrimSpace(lookedUp.HomeDir) != "" {
		return lookedUp.HomeDir
	}

	if current := currentDesktopUser(); username == current {
		if value, err := os.UserHomeDir(); err == nil {
			return value
		}
	}

	return filepath.Join("/home", username)
}

func currentDesktopUser() string {
	if value := strings.TrimSpace(os.Getenv("CARACAL_SETUP_TARGET_USER")); value != "" {
		return value
	}
	for _, envKey := range []string{"SUDO_USER", "USER"} {
		value := strings.TrimSpace(os.Getenv(envKey))
		if value != "" && userExists(value) {
			return value
		}
	}
	if current, err := user.Current(); err == nil {
		return strings.TrimSpace(current.Username)
	}
	if value := strings.TrimSpace(os.Getenv("USER")); value != "" {
		return value
	}
	return ""
}

func userExists(username string) bool {
	_, err := user.Lookup(username)
	return err == nil
}

func currentHostname() string {
	if value, err := os.Hostname(); err == nil {
		return strings.TrimSpace(value)
	}
	return ""
}

func candidateRelativePaths(start string, relative string) []string {
	var paths []string
	for dir := filepath.Clean(start); ; dir = filepath.Dir(dir) {
		paths = append(paths, filepath.Join(dir, relative))
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return paths
}
