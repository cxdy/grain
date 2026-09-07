package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const sessionFwdGuestPATH = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// SessionFwdEnv is the guest PTY environment for grain sh -A.
// agentSock is the guest unix path; proxyAddr is host:port on 127.0.0.1;
// agentBin is the grain-agent executable used as ssh ProxyCommand.
// wrapDir, when set, is prepended to PATH and used as GIT_SSH_COMMAND so stock
// ssh (not only git) hits the session wrapper.
func SessionFwdEnv(agentSock, proxyAddr, agentBin, wrapDir string) []string {
	httpProxy := "http://" + proxyAddr
	socks := "socks5h://" + proxyAddr
	if agentBin == "" {
		agentBin = "grain-agent"
	}
	gitSSH := fmt.Sprintf(
		"ssh -o ForwardAgent=no -o ProxyCommand=%s",
		shellSingleQuote(agentBin+" socks-connect %h %p"),
	)
	env := []string{
		"SSH_AUTH_SOCK=" + agentSock,
		"HTTP_PROXY=" + httpProxy,
		"HTTPS_PROXY=" + httpProxy,
		"http_proxy=" + httpProxy,
		"https_proxy=" + httpProxy,
		"ALL_PROXY=" + socks,
		"all_proxy=" + socks,
		"GIT_SSH_COMMAND=" + gitSSH,
		"GRAIN_FWD_SOCKS=" + proxyAddr,
	}
	if wrapDir != "" {
		wrap := filepath.Join(wrapDir, "ssh")
		env = mergeShellEnv(env, []string{
			"PATH=" + wrapDir + string(os.PathListSeparator) + sessionFwdGuestPATH,
			"GIT_SSH_COMMAND=" + wrap,
			"GRAIN_FWD_WRAP=" + wrapDir,
		})
	}
	return env
}

// writeSessionSSHWrapper installs wrapDir/ssh so PATH lookup of ssh uses
// SOCKS5h + the forwarded agent. The real ssh binary is invoked by absolute path.
func writeSessionSSHWrapper(wrapDir, agentBin string) error {
	if wrapDir == "" {
		return fmt.Errorf("ssh wrapper dir is empty")
	}
	if err := os.MkdirAll(wrapDir, 0o700); err != nil {
		return err
	}
	realSSH, err := exec.LookPath("ssh")
	if err != nil || realSSH == "" {
		realSSH = "/usr/bin/ssh"
	}
	if agentBin == "" {
		agentBin = "grain-agent"
	}
	body := "#!/bin/sh\nexec " + shellSingleQuote(realSSH) +
		" -o ForwardAgent=no -o ProxyCommand=" +
		shellSingleQuote(agentBin+" socks-connect %h %p") +
		" \"$@\"\n"
	path := filepath.Join(wrapDir, "ssh")
	return os.WriteFile(path, []byte(body), 0o755)
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// extraEnvForShell merges terminal-identity query env with optional -A fwd env.
// fwdEnv is nil/empty when the session did not request client forwarding.
func extraEnvForShell(qmap map[string]string, fwdEnv []string) []string {
	extra := shellEnvFromQuery(qmap)
	if len(fwdEnv) == 0 {
		return extra
	}
	return mergeShellEnv(extra, fwdEnv)
}

func fwdWrapDirFromEnv(env []string) string {
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok && k == "GRAIN_FWD_WRAP" && v != "" {
			return v
		}
	}
	return ""
}

// loginShellArgs is argv after the shell executable. A login shell sources
// /etc/profile (and path_helper), which overwrites PATH. When wrapDir is set,
// run login -c then re-prepend wrapDir and exec an interactive non-login shell
// so `command -v ssh` is the session SOCKS wrapper.
func loginShellArgs(shell, wrapDir string) []string {
	if wrapDir == "" {
		return []string{"-l"}
	}
	script := "PATH=" + shellSingleQuote(wrapDir) + ":\"$PATH\"; export PATH; exec " +
		shellSingleQuote(shell) + " -i"
	return []string{"-lc", script}
}

func loginShellArgv(shell string, extraEnv []string) []string {
	return loginShellArgs(shell, fwdWrapDirFromEnv(extraEnv))
}
