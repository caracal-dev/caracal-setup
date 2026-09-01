package guiapp

import (
	"context"
	"fmt"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
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

type SetupRequest struct{}

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

func (a *App) HasNetworkConnection() bool {
	return hasNetworkConnection()
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

	targetHome := homeDirForUser(currentUser)
	if targetHome == "" {
		targetHome = filepath.Join("/home", currentUser)
	}

	a.emitPhase("first-run", "Mandatory Setup", "running", "Opening a terminal to run ujust first-run...")
	if err := runUjustInTerminal(currentUser, targetHome, ujustTerminalRun{
		Command:        "first-run",
		Heading:        "Caracal Setup",
		Intro:          "The mandatory first-run setup is starting now.",
		SuccessMessage: "First-run setup finished successfully.",
		FailureMessage: "First-run setup failed",
		ReturnPrompt:   "Press any key to return to Caracal Setup...",
	}); err != nil {
		a.emitPhase("first-run", "Mandatory Setup", "error", err.Error())
		return SetupResult{}, err
	}
	a.emitPhase("first-run", "Mandatory Setup", "complete", "ujust first-run finished successfully.")
	a.emitPhase("finish", "Reboot", "ready", "Setup is complete. Reboot now to finish applying group and session changes.")

	return SetupResult{
		AppliedUsername: currentUser,
		AppliedHome:     targetHome,
		AppliedHostname: currentHostname(),
		RebootRequired:  true,
	}, nil
}

func (a *App) RunUpgrade() (SetupResult, error) {
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

	targetHome := homeDirForUser(currentUser)
	if targetHome == "" {
		targetHome = filepath.Join("/home", currentUser)
	}

	a.emitPhase("upgrade", "Update Caracal", "running", "Opening a terminal to run ujust upgrade...")
	if err := runUjustInTerminal(currentUser, targetHome, ujustTerminalRun{
		Command:        "upgrade",
		Heading:        "Caracal Update",
		Intro:          "The Caracal update is starting now.",
		SuccessMessage: "Caracal update finished successfully.",
		FailureMessage: "Caracal update failed",
		ReturnPrompt:   "Press any key to return to Caracal Setup...",
	}); err != nil {
		a.emitPhase("upgrade", "Update Caracal", "error", err.Error())
		return SetupResult{}, err
	}
	a.emitPhase("upgrade", "Update Caracal", "complete", "ujust upgrade finished successfully.")
	a.emitPhase("finish", "Reboot", "ready", "Update is complete. Reboot if the updater requested it.")

	return SetupResult{
		AppliedUsername: currentUser,
		AppliedHome:     targetHome,
		AppliedHostname: currentHostname(),
		RebootRequired:  false,
	}, nil
}

// ImageOption describes a Caracal OS image available for switching.
type ImageOption struct {
	Label       string `json:"label"`
	ImageName   string `json:"imageName"`
	Description string `json:"description"`
	Recommended bool   `json:"recommended"`
}

// HasLaunchCompleted reports whether the mandatory first-run has already been
// run by checking if ~/.local/share/caracal/setup-launch-version contains "launched".
func (a *App) HasLaunchCompleted() bool {
	user := currentDesktopUser()
	if user == "" {
		return false
	}
	home := homeDirForUser(user)
	if home == "" {
		return false
	}
	path := filepath.Join(home, ".local/share/caracal/setup-launch-version")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "launched")
}

// GetCurrentImageName returns the image name of the currently booted ostree
// deployment (e.g. "caracal", "caracal-dx", "caracal-stage").
func (a *App) GetCurrentImageName() string {
	out, err := exec.Command("rpm-ostree", "status", "--json").Output()
	if err != nil {
		return ""
	}
	var status struct {
		Deployments []struct {
			Booted bool   `json:"booted"`
			Origin string `json:"origin"`
		} `json:"deployments"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return ""
	}
	marker := "caracal-dev/"
	for _, dep := range status.Deployments {
		if dep.Booted {
			if idx := strings.Index(dep.Origin, marker); idx >= 0 {
				after := dep.Origin[idx+len(marker):]
				if tagIdx := strings.LastIndex(after, ":"); tagIdx >= 0 {
					return after[:tagIdx]
				}
				return after
			}
		}
	}
	return ""
}

// DetectNvidia checks whether the system has NVIDIA hardware by probing lspci
// and loaded kernel modules.
func (a *App) DetectNvidia() bool {
	out, err := exec.Command("lspci", "-nn").Output()
	if err == nil && strings.Contains(strings.ToLower(string(out)), "nvidia") {
		return true
	}
	data, err := os.ReadFile("/proc/modules")
	if err == nil && strings.Contains(string(data), "nvidia") {
		return true
	}
	return false
}

// GetAvailableImages returns the list of Caracal OS images available for
// switching. The Recommended flag is set based on NVIDIA hardware detection.
func (a *App) GetAvailableImages() []ImageOption {
	hasNvidia := a.DetectNvidia()
	return []ImageOption{
		{
			Label:       "Caracal",
			ImageName:   "caracal",
			Description: "Standard Caracal OS image",
			Recommended: !hasNvidia,
		},
		{
			Label:       "Caracal (NVIDIA)",
			ImageName:   "caracal-nvidia",
			Description: "Caracal with NVIDIA driver support",
			Recommended: hasNvidia,
		},
		{
			Label:       "Caracal Stage",
			ImageName:   "caracal-stage",
			Description: "Image for portable devices. Uses Wayfire desktop environment",
		},
		{
			Label:       "Caracal Developer Experience",
			ImageName:   "caracal-dx",
			Description: "Caracal with development tooling pre-installed",
			Recommended: !hasNvidia,
		},
		{
			Label:       "Caracal Developer Experience (NVIDIA)",
			ImageName:   "caracal-dx-nvidia",
			Description: "Caracal DX with NVIDIA driver support",
			Recommended: hasNvidia,
		},
		{
			Label:       "Caracal Gaming",
			ImageName:   "caracal-gaming",
			Description: "Heavy all-in-one Caracal based on Bazzite. For music production and gaming",
			Recommended: !hasNvidia,
		},
		{
			Label:       "Caracal Gaming (NVIDIA)",
			ImageName:   "caracal-gaming-nvidia",
			Description: "Bazzite-based Caracal with NVIDIA driver support. For music production and gaming",
			Recommended: hasNvidia,
		},
	}
}

// RebaseImage performs an ostree rebase to the specified image, opening a
// terminal window to show progress. Uses run0 for passwordless privilege
// escalation via polkit.
func (a *App) RebaseImage(targetImage string) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return fmt.Errorf("a rebase is already running")
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
		return fmt.Errorf("could not determine the current desktop user")
	}

	targetHome := homeDirForUser(currentUser)
	if targetHome == "" {
		targetHome = filepath.Join("/home", currentUser)
	}

	imageLabel := imageDisplayName(targetImage)
	heading := fmt.Sprintf("Switching to %s", imageLabel)
	headingLine := strings.Repeat("=", len(heading))

	a.emitPhase("rebase", "Switch Version", "running", "Opening a terminal to run the version switch...")

	script := strings.Join([]string{
		"printf '\\n%s\\n%s\\n\\n' " + shellQuote(heading) + " " + shellQuote(headingLine),
		"echo " + shellQuote("rpm-ostree rebase to ostree-unverified-registry:ghcr.io/caracal-dev/" + targetImage + ":latest"),
		`echo`,
		`echo 'Authorize the polkit prompt to proceed.'`,
		`echo`,
		`run0 rpm-ostree rebase ostree-unverified-registry:ghcr.io/caracal-dev/` + shellQuote(targetImage) + `:latest`,
		`status=$?`,
		`echo`,
		`if [[ $status -eq 0 ]]; then`,
		`  echo "Rebase completed successfully."`,
		`  echo "The new image will be used on the next boot."`,
		`else`,
		`  echo "Rebase failed with exit code $status."`,
		`fi`,
		`echo`,
		`read -r -n 1 -s -p "Press any key to return to Caracal Setup..."`,
		`exit $status`,
	}, "\n")

	terminal, err := findTerminal()
	if err != nil {
		return err
	}

	args := append([]string(nil), terminal.Args(targetHome, script)...)
	cmd, err := terminalCommand(terminal.Command, args)
	if err != nil {
		return err
	}
	cmd.Env = withUserEnvironment(os.Environ(), currentUser, targetHome)
	cmd.Dir = targetHome
	if os.Geteuid() == 0 && currentUser != "" && currentUser != "root" {
		if err := runCommandAsUser(cmd, currentUser, targetHome); err != nil {
			return err
		}
	}

	if err := cmd.Run(); err != nil {
		a.emitPhase("rebase", "Switch Version", "error", fmt.Sprintf("Rebase to %s failed.", imageLabel))
		return fmt.Errorf("rebase to %s did not complete successfully: %w", imageLabel, err)
	}

	a.emitPhase("rebase", "Switch Version", "complete", fmt.Sprintf("Rebase to %s completed. Reboot to apply.", imageLabel))
	a.emitPhase("finish", "Reboot", "ready", "Rebase completed. Reboot to apply the new image.")

	return nil
}

func imageDisplayName(imageName string) string {
	switch imageName {
	case "caracal":
		return "Caracal"
	case "caracal-nvidia":
		return "Caracal (NVIDIA)"
	case "caracal-stage":
		return "Caracal Stage"
	case "caracal-dx":
		return "Caracal Developer Experience"
	case "caracal-dx-nvidia":
		return "Caracal Developer Experience (NVIDIA)"
	case "caracal-gaming":
		return "Caracal Gaming"
	case "caracal-gaming-nvidia":
		return "Caracal Gaming (NVIDIA)"
	default:
		return imageName
	}
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

type ujustTerminalRun struct {
	Command        string
	Heading        string
	Intro          string
	SuccessMessage string
	FailureMessage string
	ReturnPrompt   string
}

func runUjustInTerminal(targetUser string, targetHome string, run ujustTerminalRun) error {
	terminal, err := findTerminal()
	if err != nil {
		return err
	}

	headingLine := strings.Repeat("=", len(run.Heading))
	script := strings.Join([]string{
		"printf '\\n%s\\n%s\\n\\n' " + shellQuote(run.Heading) + " " + shellQuote(headingLine),
		"echo " + shellQuote(run.Intro),
		`echo 'Finish any prompts in this terminal window.'`,
		`echo`,
		`if ! command -v brew >/dev/null 2>&1 && [[ -x /home/linuxbrew/.linuxbrew/bin/brew ]]; then`,
		`  eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"`,
		`fi`,
		"ujust " + shellQuote(run.Command),
		`status=$?`,
		`echo`,
		`if [[ $status -eq 0 ]]; then`,
		"  echo " + shellQuote(run.SuccessMessage),
		`else`,
		`  echo ` + shellQuote(run.FailureMessage) + ` "with exit code $status."`,
		`fi`,
		`echo`,
		"read -r -n 1 -s -p " + shellQuote(run.ReturnPrompt),
		`echo`,
		`exit $status`,
	}, "\n")

	args := append([]string(nil), terminal.Args(targetHome, script)...)
	cmd, err := terminalCommand(terminal.Command, args)
	if err != nil {
		return err
	}
	cmd.Env = withUserEnvironment(os.Environ(), targetUser, targetHome)
	cmd.Dir = targetHome
	if os.Geteuid() == 0 && targetUser != "" && targetUser != "root" {
		if err := runCommandAsUser(cmd, targetUser, targetHome); err != nil {
			return err
		}
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ujust %s did not complete successfully: %w", run.Command, err)
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

	filtered = setEnvValue(filtered, "PATH", homebrewPath(envValue(filtered, "PATH")))
	filtered = append(
		filtered,
		"HOME="+home,
		"USER="+username,
		"LOGNAME="+username,
		"PWD="+home,
	)
	return filtered
}

func runCommandAsUser(cmd *exec.Cmd, username string, home string) error {
	envArgs := append(environmentAssignments(cmd.Env, username, home), cmd.Args...)

	if runuserPath, err := exec.LookPath("runuser"); err == nil {
		cmd.Path = runuserPath
		cmd.Args = append([]string{"runuser", "--preserve-environment", "-u", username, "--", "env"}, envArgs...)
		return nil
	}

	if sudoPath, err := exec.LookPath("sudo"); err == nil {
		cmd.Path = sudoPath
		cmd.Args = append([]string{"sudo", "-u", username, "--", "env"}, envArgs...)
		return nil
	}

	return fmt.Errorf("caracal-setup is running as root and cannot locate runuser or sudo to launch ujust as %s", username)
}

func terminalCommand(command string, args []string) (*exec.Cmd, error) {
	switch command {
	case "ghostty":
		return exec.Command("ghostty", args...), nil
	case "konsole":
		return exec.Command("konsole", args...), nil
	case "gnome-terminal":
		return exec.Command("gnome-terminal", args...), nil
	case "ptyxis":
		return exec.Command("ptyxis", args...), nil
	case "kgx":
		return exec.Command("kgx", args...), nil
	case "kitty":
		return exec.Command("kitty", args...), nil
	case "wezterm":
		return exec.Command("wezterm", args...), nil
	case "xfce4-terminal":
		return exec.Command("xfce4-terminal", args...), nil
	case "mate-terminal":
		return exec.Command("mate-terminal", args...), nil
	case "lxterminal":
		return exec.Command("lxterminal", args...), nil
	case "x-terminal-emulator":
		return exec.Command("x-terminal-emulator", args...), nil
	case "xterm":
		return exec.Command("xterm", args...), nil
	default:
		return nil, fmt.Errorf("unsupported terminal command: %s", command)
	}
}

func environmentAssignments(env []string, username string, home string) []string {
	assignments := make([]string, 0, len(env)+4)
	for _, item := range env {
		if strings.Contains(item, "=") {
			assignments = append(assignments, item)
		}
	}
	return append(assignments,
		"HOME="+home,
		"USER="+username,
		"LOGNAME="+username,
		"PWD="+home,
	)
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func setEnvValue(env []string, key string, value string) []string {
	prefix := key + "="
	for index, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[index] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func homebrewPath(pathValue string) string {
	const brewBin = "/home/linuxbrew/.linuxbrew/bin"
	if pathValue == "" {
		return brewBin + ":/usr/local/bin:/usr/bin:/bin"
	}
	parts := strings.Split(pathValue, ":")
	for _, part := range parts {
		if part == brewBin {
			return pathValue
		}
	}
	return brewBin + ":" + pathValue
}

func hasNetworkConnection() bool {
	client := http.Client{
		Timeout: 3 * time.Second,
	}
	for _, endpoint := range []string{
		"https://www.google.com/generate_204",
		"https://connectivitycheck.gstatic.com/generate_204",
		"https://github.com",
		"https://raw.githubusercontent.com",
		"https://1.1.1.1",
	} {
		request, err := http.NewRequest(http.MethodHead, endpoint, nil)
		if err != nil {
			continue
		}
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 500 {
				return true
			}
		}
	}
	return false
}

func runPrivilegedCommand(stdin io.Reader, args ...string) error {
	if _, err := exec.LookPath("pkexec"); err != nil {
		return fmt.Errorf("pkexec is not installed")
	}
	if _, err := exec.LookPath("env"); err != nil {
		return fmt.Errorf("could not locate env for pkexec wrapper: %w", err)
	}

	cmdArgs := append([]string{"env"}, args...)
	cmd := exec.Command("pkexec", cmdArgs...)
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
			Command: "ghostty",
			Args: func(home string, script string) []string {
				return []string{"--working-directory", home, "-e", "bash", "-lc", script}
			},
		},
		{
			Command: "konsole",
			Args: func(home string, script string) []string {
				return []string{"--workdir", home, "-e", "bash", "-lc", script}
			},
		},
	}
	candidates = append(candidates, preferredTerminalCandidates()...)
	candidates = append(candidates, []terminalCandidate{
		{
			Command: "gnome-terminal",
			Args: func(home string, script string) []string {
				return []string{"--working-directory", home, "--", "bash", "-lc", script}
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

	return terminalCandidate{}, fmt.Errorf("no supported terminal emulator was found to run ujust commands; install a desktop terminal such as Ghostty, Konsole, or GNOME Terminal")
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
		return commandToTerminalCandidate("ghostty")
	case "com.mitchellh.ghostty.desktop":
		return commandToTerminalCandidate("ghostty")
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

	return terminalCandidate{}, false
}

func commandToTerminalCandidate(command string) (terminalCandidate, bool) {
	switch filepath.Base(command) {
	case "ghostty":
		return terminalCandidate{
			Command: "ghostty",
			Args: func(home string, script string) []string {
				return []string{"--working-directory", home, "-e", "bash", "-lc", script}
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
		if isRegularUser(value) {
			return value
		}
	}
	for _, envKey := range []string{"SUDO_USER", "LOGNAME", "USER"} {
		value := strings.TrimSpace(os.Getenv(envKey))
		if value != "" && isRegularUser(value) {
			return value
		}
	}
	if value := userFromPKExecUID(); value != "" {
		return value
	}
	if out, err := exec.Command("logname").Output(); err == nil {
		value := strings.TrimSpace(string(out))
		if isRegularUser(value) {
			return value
		}
	}
	if current, err := user.Current(); err == nil {
		value := strings.TrimSpace(current.Username)
		if isRegularUser(value) {
			return value
		}
	}
	return ""
}

func isRegularUser(username string) bool {
	lookedUp, err := user.Lookup(username)
	if err != nil {
		return false
	}
	uid, err := strconv.Atoi(lookedUp.Uid)
	return err == nil && uid >= 1000 && username != "root"
}

func userFromPKExecUID() string {
	value := strings.TrimSpace(os.Getenv("PKEXEC_UID"))
	if value == "" {
		return ""
	}
	lookedUp, err := user.LookupId(value)
	if err != nil {
		return ""
	}
	if isRegularUser(lookedUp.Username) {
		return lookedUp.Username
	}
	return ""
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
