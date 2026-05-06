package guiapp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var usernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

type App struct {
	ctx context.Context

	mu      sync.Mutex
	running bool
}

type ProfileView struct {
	CurrentUsername string `json:"currentUsername"`
	CurrentHome     string `json:"currentHome"`
}

type SetupRequest struct {
	ChangeAccount bool   `json:"changeAccount"`
	Username      string `json:"username"`
	Password      string `json:"password"`
}

type SetupResult struct {
	AppliedUsername string `json:"appliedUsername"`
	AppliedHome     string `json:"appliedHome"`
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

	targetUser := currentUser
	if request.ChangeAccount {
		targetUser = strings.TrimSpace(request.Username)
		if err := validateSetupRequest(currentUser, request); err != nil {
			a.emitPhase("account", "Account Details", "error", err.Error())
			return SetupResult{}, err
		}

		a.emitPhase("account", "Account Details", "running", "Applying your username and password changes...")
		if err := runAccountUpdate(currentUser, targetUser, request.Password); err != nil {
			a.emitPhase("account", "Account Details", "error", err.Error())
			return SetupResult{}, err
		}
		a.emitPhase("account", "Account Details", "complete", "Account details updated.")
	} else {
		a.emitPhase("account", "Account Details", "complete", "Keeping the current username and password.")
	}

	targetHome := homeDirForUser(targetUser)
	if targetHome == "" {
		targetHome = filepath.Join("/home", targetUser)
	}

	a.emitPhase("first-run", "Mandatory Setup", "running", "Opening a terminal to run ujust first-run...")
	if err := runFirstRunInTerminal(targetUser, targetHome); err != nil {
		a.emitPhase("first-run", "Mandatory Setup", "error", err.Error())
		return SetupResult{}, err
	}
	a.emitPhase("first-run", "Mandatory Setup", "complete", "ujust first-run finished successfully.")
	a.emitPhase("finish", "Reboot", "ready", "Setup is complete. Reboot now to finish applying group and session changes.")

	return SetupResult{
		AppliedUsername: targetUser,
		AppliedHome:     targetHome,
		RebootRequired:  true,
	}, nil
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

func validateSetupRequest(currentUser string, request SetupRequest) error {
	username := strings.TrimSpace(request.Username)
	switch {
	case username == "":
		return fmt.Errorf("enter a username or skip this step")
	case !usernamePattern.MatchString(username):
		return fmt.Errorf("use a lowercase username starting with a letter or underscore")
	case request.Password == "":
		return fmt.Errorf("enter a password or skip this step")
	case strings.ContainsRune(request.Password, '\n'):
		return fmt.Errorf("password cannot contain newlines")
	}

	if username == currentUser {
		return nil
	}

	if _, err := user.Lookup(username); err == nil {
		return fmt.Errorf("the username %q already exists", username)
	}

	return nil
}

func runAccountUpdate(currentUser string, targetUser string, password string) error {
	script, err := resolveScriptPath("apply-account-settings.sh")
	if err != nil {
		return err
	}

	stdin := bytes.NewBufferString(password + "\n")
	if err := runPrivilegedCommand(stdin, script, currentUser, targetUser); err != nil {
		return fmt.Errorf("could not update account details: %w", err)
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
	candidates := []terminalCandidate{
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
			Command: "kitty",
			Args: func(home string, script string) []string {
				return []string{"--directory", home, "bash", "-lc", script}
			},
		},
		{
			Command: "xterm",
			Args: func(home string, script string) []string {
				return []string{"-T", "Caracal Setup", "-e", "bash", "-lc", script}
			},
		},
	}

	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate.Command); err == nil {
			return candidate, nil
		}
	}

	return terminalCandidate{}, fmt.Errorf("no supported terminal emulator was found to run ujust first-run")
}

func homeDirForUser(username string) string {
	if username == "" {
		if value, err := os.UserHomeDir(); err == nil {
			return value
		}
		return ""
	}

	if current := currentDesktopUser(); username == current {
		if value, err := os.UserHomeDir(); err == nil {
			return value
		}
	}

	if lookedUp, err := user.Lookup(username); err == nil && strings.TrimSpace(lookedUp.HomeDir) != "" {
		return lookedUp.HomeDir
	}

	return filepath.Join("/home", username)
}

func currentDesktopUser() string {
	if value := strings.TrimSpace(os.Getenv("CARACAL_SETUP_TARGET_USER")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("SUDO_USER")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("USER")); value != "" {
		return value
	}
	if current, err := user.Current(); err == nil {
		return strings.TrimSpace(current.Username)
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
