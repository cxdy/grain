package guest

import "fmt"

// quietSSHOpts are common OpenSSH options for grain guest access:
// suppress host-key noise and only use the identity we pass.
func quietSSHOpts() []string {
	return []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "UpdateHostKeys=no",
		"-o", "LogLevel=ERROR",
		"-o", "IdentitiesOnly=yes",
	}
}

// SSHArgs returns full arguments after "ssh" for an interactive or remote command:
// quiet opts, optional -i identity, -p port, and user@host.
func SSHArgs(user, host string, port int, identity string) []string {
	args := quietSSHOpts()
	if identity != "" {
		args = append(args, "-i", identity)
	}
	if port > 0 {
		args = append(args, "-p", fmt.Sprintf("%d", port))
	}
	args = append(args, fmt.Sprintf("%s@%s", user, host))
	return args
}

// SCPArgs returns scp option flags only (caller appends src and dst).
// Uses -P for port (scp convention).
func SCPArgs(port int, identity string) []string {
	args := quietSSHOpts()
	if identity != "" {
		args = append(args, "-i", identity)
	}
	if port > 0 {
		args = append(args, "-P", fmt.Sprintf("%d", port))
	}
	return args
}

// SSHProbeArgs returns full arguments after "ssh" for a readiness probe:
// quiet opts plus BatchMode and ConnectTimeout, -i, -p, user@host, --, true.
func SSHProbeArgs(user, host string, port int, identity string) []string {
	args := quietSSHOpts()
	args = append(args,
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=3",
	)
	if identity != "" {
		args = append(args, "-i", identity)
	}
	if port > 0 {
		args = append(args, "-p", fmt.Sprintf("%d", port))
	}
	args = append(args, fmt.Sprintf("%s@%s", user, host), "--", "true")
	return args
}
