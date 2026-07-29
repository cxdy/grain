package vm

// CreateEvent is a progress update during Manager.Create.
// Phases: image | disk | seed | qemu | wait_ssh | wait_agent | userdata | bootstrap | ready | error
type CreateEvent struct {
	Phase   string `json:"phase"`
	Message string `json:"message,omitempty"`
	Name    string `json:"name,omitempty"`
	Error   string `json:"error,omitempty"`
	// SSHPort is set on ready (and optionally earlier once known).
	SSHPort int `json:"ssh_port,omitempty"`
	// Instance is set on the final ready event.
	Instance *Instance `json:"instance,omitempty"`
}

// Create phase constants.
const (
	PhaseImage     = "image"
	PhaseDisk      = "disk"
	PhaseSeed      = "seed"
	PhaseQEMU      = "qemu"
	PhaseWaitSSH   = "wait_ssh"
	PhaseWaitAgent = "wait_agent"
	PhaseUserdata  = "userdata"
	PhaseBootstrap = "bootstrap"
	PhaseReady     = "ready"
	PhaseError     = "error"
)
