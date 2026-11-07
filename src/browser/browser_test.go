package browser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
	"github.com/federico-pepe/push-hack-sample-edit/src/wavio"
)

func setup(t *testing.T) (*Store, string) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Drums"), 0o755)
	os.MkdirAll(filepath.Join(root, "_rec"), 0o755)
	for _, n := range []string{"b.wav", "A.WAV", "Drums/kick.wav"} {
		b := audio.New(48000, 2, 4800)
		for i := range b.Ch[0] {
			b.Ch[0][i] = 0.1
		}
		if err := wavio.Write(filepath.Join(root, n), b, &wavio.Meta{Root: 60, Loops: []wavio.Loop{{Start: 1, End: 100}}}); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, ".hidden.wav"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "broken.wav"), []byte("junk"), 0o644)
	return &Store{Root: root}, root
}

func TestListOrderFiltersAndInfo(t *testing.T) {
	s, root := setup(t)
	es, err := s.List(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name)
	}
	want := []string{"Drums", "A.WAV", "b.wav", "broken.wav"}
	if len(names) != len(want) {
		t.Fatalf("%v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("%v want %v", names, want)
		}
	}
	if !es[0].Dir || es[1].Info.Frames != 4800 || es[1].Info.Rate != 48000 || es[1].Info.Channels != 2 || es[1].Info.Bits != 24 {
		t.Fatalf("%+v", es[1])
	}
	if es[1].Seconds() != 0.1 {
		t.Fatalf("seconds %v", es[1].Seconds())
	}
	if !es[3].Bad {
		t.Fatal("broken file should be flagged")
	}
}

func TestPathSafety(t *testing.T) {
	s, root := setup(t)
	for _, p := range []string{filepath.Join(root, "..", "x"), "/etc/passwd", root + "-evil", filepath.Join(root, "Drums", "..", "..", "etc")} {
		if _, err := s.List(p); err == nil {
			t.Fatalf("List allowed %q", p)
		}
		if s.Delete(p) == nil {
			t.Fatalf("Delete allowed %q", p)
		}
		if _, err := s.Copy(p, root); err == nil {
			t.Fatalf("Copy src allowed %q", p)
		}
		if _, err := s.Read(p); err == nil {
			t.Fatalf("Read allowed %q", p)
		}
	}
	if _, err := s.Copy(filepath.Join(root, "b.wav"), "/tmp"); err == nil {
		t.Fatal("Copy dst outside root")
	}
	if s.Parent(root) != root || s.Parent(filepath.Join(root, "Drums")) != root || s.Parent("/etc") != root {
		t.Fatal("parent must stop at the root")
	}
}

func TestCopyNamesAndKeepsTags(t *testing.T) {
	s, root := setup(t)
	src := filepath.Join(root, "b.wav")
	var got []string
	for i := 0; i < 3; i++ {
		p, err := s.Copy(src, root)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, filepath.Base(p))
	}
	if got[0] != "b copy.wav" || got[1] != "b copy 2.wav" || got[2] != "b copy 3.wav" {
		t.Fatalf("%v", got)
	}
	p, err := s.Copy(src, filepath.Join(root, "Drums"))
	if err != nil || filepath.Base(p) != "b.wav" {
		t.Fatalf("%v %v", p, err)
	}
	a, _ := os.ReadFile(src)
	b, _ := os.ReadFile(p)
	if string(a) != string(b) {
		t.Fatal("copy must be byte-identical (tags kept)")
	}
	if _, err := s.Copy(filepath.Join(root, "Drums"), root); err == nil {
		t.Fatal("folders cannot be copied")
	}
}

func TestDeleteRules(t *testing.T) {
	s, root := setup(t)
	if err := s.Delete(filepath.Join(root, "b.wav")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "b.wav")); !os.IsNotExist(err) {
		t.Fatal("not deleted")
	}
	if s.Delete(filepath.Join(root, "Drums")) == nil || s.Delete(root) == nil || s.Delete(filepath.Join(root, "notes.txt")) == nil {
		t.Fatal("folders, the root and non-wav files must be refused")
	}
	if s.Delete(filepath.Join(root, "missing.wav")) == nil {
		t.Fatal("missing file should error")
	}
}

func TestReadLimits(t *testing.T) {
	s, root := setup(t)
	b, err := s.Read(filepath.Join(root, "A.WAV"))
	if err != nil || b.Frames() != 4800 {
		t.Fatalf("%v %v", err, b)
	}
	if _, err := s.Read(filepath.Join(root, "Drums")); err == nil {
		t.Fatal("folder read")
	}
	if _, err := s.Read(filepath.Join(root, "broken.wav")); err == nil {
		t.Fatal("broken read")
	}
}
