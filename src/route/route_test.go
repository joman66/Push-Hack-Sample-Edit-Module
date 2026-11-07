package route

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAndEnv(t *testing.T) {
	if err := Check("hw:0,0"); err != nil {
		t.Fatal("hardware names always pass")
	}
	dir := t.TempDir()
	conf := filepath.Join(dir, "alsa.conf")
	t.Setenv("PUSH_HACK_ALSA_CONF", conf)
	if err := Check("ph_in_sample-edit_in"); err == nil || !strings.Contains(err.Error(), "push-manager") {
		t.Fatalf("missing config: %v", err)
	}
	os.WriteFile(conf, []byte("pcm.ph_in_other_in {\n}\n"), 0o644)
	if err := Check("ph_in_sample-edit_in"); err == nil || !strings.Contains(err.Error(), "connect a source") {
		t.Fatalf("no route: %v", err)
	}
	os.WriteFile(conf, []byte("pcm.ph_in_sample-edit_in {\n}\n"), 0o644)
	if err := Check("ph_in_sample-edit_in"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALSA_CONFIG_PATH", "/elsewhere")
	n := 0
	for _, e := range Env("ph_in_sample-edit_in") {
		if strings.HasPrefix(e, "ALSA_CONFIG_PATH=") {
			n++
			if e != "ALSA_CONFIG_PATH="+conf {
				t.Fatal(e)
			}
		}
	}
	if n != 1 {
		t.Fatalf("want one ALSA_CONFIG_PATH, got %d", n)
	}
	for _, e := range Env("hw:0,0") {
		if e == "ALSA_CONFIG_PATH="+conf {
			t.Fatal("hardware device must not get the matrix config")
		}
	}
}
