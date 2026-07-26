package cli

import (
	"fmt"
	"strings"

	"github.com/cxdy/grain/internal/cloudinit"
	"github.com/cxdy/grain/internal/presets"
	"github.com/cxdy/grain/internal/vm"
)

// applyPreset merges named preset userdata into user-supplied userdata and
// applies preset side effects (k3s resources / API port forward).
//
// cpusSet/memSet indicate whether the caller already chose resources (flag or
// profile); when false, preset default resources may fill zeros.
func applyPreset(presetName, userdata string, cpus, mem int, cpusSet, memSet bool, fwds []vm.PortForward) (
	outUserdata string, outCPUs, outMem int, outFwds []vm.PortForward, err error,
) {
	outCPUs, outMem, outFwds = cpus, mem, fwds
	presetName = strings.TrimSpace(presetName)
	if presetName == "" {
		return userdata, outCPUs, outMem, outFwds, nil
	}

	presetUD, err := presets.Get(presetName)
	if err != nil {
		return "", 0, 0, nil, err
	}

	// Merge: preset as base, user userdata as extra (user overrides / appends).
	if strings.TrimSpace(userdata) == "" {
		outUserdata = presetUD
	} else {
		outUserdata, err = cloudinit.MergeUserData(presetUD, userdata)
		if err != nil {
			return "", 0, 0, nil, fmt.Errorf("merge preset %q userdata: %w", presetName, err)
		}
	}

	// k3s (and future) resource suggestions when still unset.
	if dc, dm := presets.DefaultResources(presetName); dc > 0 || dm > 0 {
		if !cpusSet && outCPUs <= 0 && dc > 0 {
			outCPUs = dc
		}
		if !memSet && outMem <= 0 && dm > 0 {
			outMem = dm
		}
	}

	// k3s: auto-publish guest 6443 if not already present.
	if presets.NeedsAPIServerPort(presetName) && !hasGuestPort(outFwds, 6443) {
		outFwds = append(append([]vm.PortForward(nil), outFwds...), vm.PortForward{
			HostPort:  0,
			GuestPort: 6443,
			Proto:     "tcp",
		})
	}

	return outUserdata, outCPUs, outMem, outFwds, nil
}

func hasGuestPort(fwds []vm.PortForward, guest int) bool {
	for _, f := range fwds {
		if f.GuestPort == guest {
			return true
		}
	}
	return false
}
