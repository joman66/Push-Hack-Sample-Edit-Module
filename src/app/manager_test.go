package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
	"github.com/federico-pepe/push-hack-sample-edit/src/browser"
	"github.com/federico-pepe/push-hack-sample-edit/src/wavio"
)

// mgrApp makes an app whose sample folder holds: Drums/ (hat.wav), kick.wav, snare.wav.
func mgrApp(t *testing.T) (*App, string) {
	t.Helper()
	a, _ := newApp(t)
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Drums"), 0o755)
	for _, n := range []string{"kick.wav", "snare.wav", "Drums/hat.wav"} {
		b := tone(2400)
		if err := wavio.Write(filepath.Join(root, n), b, nil); err != nil {
			t.Fatal(err)
		}
	}
	a.H.Files = &browser.Store{Root: root}
	a.H.StartDir = root
	return a, root
}

func openMgr(a *App) { top(a, 0); bot(a, 6) }

func TestLoadKeyOpensManagerOnRecordPage(t *testing.T) {
	a, _ := mgrApp(t)
	top(a, 1)
	bot(a, 6) // Trim page: key 7 is Paste, not the manager
	if a.mgr != nil {
		t.Fatal("manager should open only from the Record page")
	}
	openMgr(a)
	if a.mgr == nil {
		t.Fatal("Record > Load should open the manager")
	}
	f := a.Snapshot(960)
	if f.Mgr == nil || f.Mgr.Total != 3 || f.Mgr.Rows[0].Name != "Drums" || !f.Mgr.Rows[0].Dir {
		t.Fatalf("%+v", f.Mgr)
	}
	if f.Bot[0].Label != "Load" || f.Bot[4].Label != "Delete" || f.Bot[2].Label != "Copy" || f.Bot[3].Label != "Paste" || f.Bot[1].Label != "Preview" {
		t.Fatalf("labels %+v", f.Bot)
	}
	bot(a, 7) // Back
	if a.mgr != nil {
		t.Fatal("Back closes")
	}
}

func TestNoStoreOrWhileRecording(t *testing.T) {
	a, _ := newApp(t) // no Files
	openMgr(a)
	if a.mgr != nil {
		t.Fatal("no store: no window")
	}
	a, _ = mgrApp(t)
	top(a, 0)
	bot(a, 0) // arm
	bot(a, 6)
	if a.mgr != nil {
		t.Fatal("must not open while armed")
	}
}

func TestNavigateAndLoad(t *testing.T) {
	a, root := mgrApp(t)
	openMgr(a)
	press(a, push3.CCDPadRight) // enter Drums
	if a.mgr.Dir != filepath.Join(root, "Drums") || len(a.mgr.Entries) != 1 {
		t.Fatalf("%s %v", a.mgr.Dir, a.mgr.Entries)
	}
	press(a, push3.CCDPadLeft) // up, lands on Drums
	if a.mgr.Dir != root || a.mgr.Entries[a.mgr.Cur].Name != "Drums" {
		t.Fatal("up should land on the folder we left")
	}
	press(a, push3.CCDPadLeft) // up at the root stays
	if a.mgr.Dir != root {
		t.Fatal("never above root")
	}
	press(a, push3.CCDPadDown)
	press(a, push3.CCDPadDown)
	if a.mgr.Entries[a.mgr.Cur].Name != "snare.wav" {
		t.Fatal("down x2")
	}
	turn(a, push3.CCEncoder1, -1)
	if a.mgr.Entries[a.mgr.Cur].Name != "kick.wav" {
		t.Fatal("encoder 1 moves the cursor")
	}
	bot(a, 0) // Load
	if a.mgr != nil || a.frames() != 2400 || a.Mode != ModeTrim {
		t.Fatalf("load: mgr=%v frames=%d mode=%v", a.mgr, a.frames(), a.Mode)
	}
	if a.S.Name != "kick-edit" {
		t.Fatalf("export name must not collide with the original: %s", a.S.Name)
	}
	// reopening goes back to the same folder
	a.Mode = ModeRecord
	bot(a, 6)
	if a.mgr == nil || a.mgr.Dir != root {
		t.Fatal("reopen")
	}
	// loading a folder or a broken file is refused
	a.mgr.Cur = 0
	bot(a, 0)
	if a.mgr == nil {
		t.Fatal("a folder cannot be loaded")
	}
}

func TestCopyPasteAndFolderPaste(t *testing.T) {
	a, root := mgrApp(t)
	openMgr(a)
	bot(a, 3)
	if a.msg != "Nothing copied" {
		t.Fatalf("%q", a.msg)
	}
	a.mgr.Cur = 1 // kick.wav
	bot(a, 2)
	bot(a, 3) // paste into the same folder
	if _, err := os.Stat(filepath.Join(root, "kick copy.wav")); err != nil {
		t.Fatal("paste should make 'kick copy.wav'")
	}
	if a.mgr.Entries[a.mgr.Cur].Name != "kick copy.wav" {
		t.Fatal("cursor should land on the new file")
	}
	// the Shift+Duplicate and Select shortcuts do the same
	a.mgr.Cur = 2
	shiftPress(a, push3.CCDuplicate)
	press(a, push3.CCDPadUp)
	press(a, push3.CCDPadUp)
	press(a, push3.CCDPadUp) // Drums folder
	press(a, push3.CCDPadRight)
	press(a, push3.CCSelect)
	if _, err := os.Stat(filepath.Join(root, "Drums", "kick copy.wav")); err == nil {
		t.Fatal("wrong file pasted")
	}
	if _, err := os.Stat(filepath.Join(root, "Drums", a.mgr.Entries[a.mgr.Cur].Name)); err != nil {
		t.Fatal("pasted file missing in the folder")
	}
	// a folder cannot be copied
	a.mgr.Dir = root
	a.mgrRefresh()
	a.mgr.Cur = 0
	bot(a, 2)
	if a.msg != "Pick a sample to copy" {
		t.Fatalf("%q", a.msg)
	}
}

func TestDeleteNeedsConfirm(t *testing.T) {
	a, root := mgrApp(t)
	openMgr(a)
	a.mgr.Cur = 1
	bot(a, 4)
	if _, err := os.Stat(filepath.Join(root, "kick.wav")); err != nil {
		t.Fatal("first press must not delete")
	}
	if !a.mgr.Confirm || a.Snapshot(960).Bot[4].Label != "Sure?" {
		t.Fatal("should ask to confirm")
	}
	// anything else cancels the confirm
	press(a, push3.CCDPadDown)
	if a.mgr.Confirm {
		t.Fatal("moving cancels")
	}
	a.mgr.Cur = 1
	press(a, push3.CCDelete)
	press(a, push3.CCDelete) // Delete button twice
	if _, err := os.Stat(filepath.Join(root, "kick.wav")); !os.IsNotExist(err) {
		t.Fatal("second press should delete")
	}
	if len(a.mgr.Entries) != 2 {
		t.Fatal("list should refresh")
	}
	// a copied file that is deleted leaves the clipboard
	a.mgr.Cur = 1
	bot(a, 2)
	bot(a, 4)
	bot(a, 4)
	if a.mgr.Clip != "" {
		t.Fatal("clipboard should clear")
	}
	// folders are never deleted
	a.mgr.Cur = 0
	bot(a, 4)
	bot(a, 4)
	if _, err := os.Stat(filepath.Join(root, "Drums")); err != nil {
		t.Fatal("folder removed")
	}
}

func TestPreviewDoesNotLoad(t *testing.T) {
	a, _ := mgrApp(t)
	a.Load(tone(777))
	openMgr(a)
	a.mgr.Cur = 1
	bot(a, 1)
	if !a.Eng.Playing() || a.mgr.Preview == "" {
		t.Fatal("should be playing")
	}
	if a.frames() != 777 {
		t.Fatal("preview must not touch the editor's sample")
	}
	if a.Snapshot(960).Bot[1].Label != "Stop" {
		t.Fatal("button should say Stop")
	}
	bot(a, 1)
	if a.Eng.Playing() {
		t.Fatal("stop")
	}
	// moving the cursor stops a preview; a pad previews too; play button too
	bot(a, 1)
	press(a, push3.CCDPadDown)
	if a.Eng.Playing() {
		t.Fatal("moving stops the preview")
	}
	a.Pad(push3.PadNote(0, 0), 100)
	if !a.Eng.Playing() {
		t.Fatal("pad previews")
	}
	press(a, push3.CCPlay)
	if a.Eng.Playing() {
		t.Fatal("play toggles")
	}
	// a preview that plays out clears its flag on Tick
	bot(a, 1)
	out := make([]float32, 2*20000)
	a.Eng.Render(out)
	a.Tick()
	if a.mgr.Preview != "" {
		t.Fatal("finished preview should clear")
	}
	// closing stops sound
	bot(a, 1)
	bot(a, 7)
	if a.Eng.Playing() {
		t.Fatal("closing stops preview")
	}
}

func TestModeKeysLeaveManagerAndSafeRender(t *testing.T) {
	a, root := mgrApp(t)
	openMgr(a)
	top(a, 1)
	if a.mgr != nil || a.Mode != ModeTrim {
		t.Fatal("a mode key closes the manager")
	}
	openMgrFromTrim := func() { top(a, 0); bot(a, 6) }
	openMgrFromTrim()
	// folder removed under us: falls back to a folder above
	os.RemoveAll(filepath.Join(root, "Drums"))
	a.mgr.Dir = filepath.Join(root, "Drums")
	bot(a, 6) // Refresh
	if a.mgr.Dir != root {
		t.Fatalf("fallback: %s", a.mgr.Dir)
	}
	// every page and the manager render ASCII only
	f := a.Snapshot(960)
	for _, r := range f.Mgr.Rows {
		for _, c := range r.Name + r.Detail {
			if c > 126 {
				t.Fatal("non-ASCII on screen")
			}
		}
	}
	if len(a.LEDs().Btn) == 0 {
		t.Fatal("leds")
	}
}

func TestAsciiNames(t *testing.T) {
	if asciiName("Kick éè.wav") != "Kick ??.wav" {
		t.Fatal(asciiName("Kick éè.wav"))
	}
	if tail("/a/very/long/path/to/Drums", 12) != ".../to/Drums" {
		t.Fatal(tail("/a/very/long/path/to/Drums", 12))
	}
}

var _ = audio.MinDB
