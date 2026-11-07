// Package route lets a capture device name come from Push Manager's audio
// routing matrix. A name that starts with "ph_" (for example
// ph_in_sample-edit_in) is a PCM that lives in the generated ALSA config.
// Stdlib only: no core dependency, so it builds with the pinned core.
package route

import (
	"errors"
	"os"
	"strings"
)

// DefaultConf is where Push Manager writes the generated ALSA config.
const DefaultConf = "/data/push-hack/audio-routing/alsa.conf"

// Prefix marks a device name that comes from the matrix.
const Prefix = "ph_"

// Routed tells if a device name is a matrix PCM.
func Routed(device string) bool { return strings.HasPrefix(device, Prefix) }

// ConfPath is the config to use. PUSH_HACK_ALSA_CONF overrides it (tests).
func ConfPath() string {
	if p := os.Getenv("PUSH_HACK_ALSA_CONF"); p != "" {
		return p
	}
	return DefaultConf
}

// Check says why a routed device cannot open, in words for the user.
// A device that is not routed always passes.
func Check(device string) error {
	if !Routed(device) {
		return nil
	}
	raw, err := os.ReadFile(ConfPath())
	if err != nil {
		return errors.New("no audio routing config: is push-manager running?")
	}
	if !strings.Contains(string(raw), "pcm."+device+" ") {
		return errors.New("nothing is routed to this input: open Push Manager, Audio, and connect a source")
	}
	return nil
}

// Env is the environment for arecord. A routed device needs
// ALSA_CONFIG_PATH. Any other device keeps the plain environment, so the
// hardware inputs behave as before.
func Env(device string) []string {
	env := os.Environ()
	if !Routed(device) {
		return env
	}
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, "ALSA_CONFIG_PATH=") {
			out = append(out, e)
		}
	}
	return append(out, "ALSA_CONFIG_PATH="+ConfPath())
}
