package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
	"github.com/federico-pepe/push-hack-sample-edit/src/browser"
	"github.com/federico-pepe/push-hack-sample-edit/src/playback"
)

// This file is the Sample Manager: a window for the samples stored on the
// device. It opens from Record > Load. It can load a sample into the editor,
// copy and paste files, delete them (with a confirm), and preview them
// without loading.

// FileStore is the device's sample folder. *browser.Store implements it.
type FileStore interface {
	List(dir string) ([]browser.Entry, error)
	Parent(dir string) string
	Read(path string) (*audio.Buffer, error)
	Copy(src, dstDir string) (string, error)
	Delete(path string) error
}

// mgrVisible is how many list rows fit on the screen.
const mgrVisible = 5

// Manager is the window's state.
type Manager struct {
	Dir     string
	Entries []browser.Entry
	Cur     int
	Scroll  int
	Clip    string // path of the copied file
	Confirm bool   // Delete was pressed once
	Preview string // path being previewed
	cache   struct {
		path string
		buf  *audio.Buffer
	}
}

func (a *App) openManager() {
	if a.H.Files == nil {
		a.say("No sample folder")
		return
	}
	if a.Rec.State() != 0 {
		a.say("Stop recording first")
		return
	}
	m := &Manager{Dir: a.mgrDir}
	if m.Dir == "" {
		m.Dir = a.H.StartDir
	}
	a.mgr = m
	a.mgrRefresh()
	a.dirty = true
}

func (a *App) closeManager() {
	if a.mgr == nil {
		return
	}
	a.mgrStopPreview()
	a.mgrDir = a.mgr.Dir
	a.mgr = nil
	a.dirty = true
}

// mgrRefresh reads the folder again. A folder that has gone is replaced by
// the nearest one above that still exists.
func (a *App) mgrRefresh() {
	m := a.mgr
	for i := 0; i < 32; i++ {
		es, err := a.H.Files.List(m.Dir)
		if err == nil {
			m.Entries = es
			break
		}
		up := a.H.Files.Parent(m.Dir)
		if up == m.Dir {
			m.Entries = nil
			a.say("Folder: %s", asciiErr(err))
			break
		}
		m.Dir = up
	}
	m.Cur = max(0, min(m.Cur, len(m.Entries)-1))
	a.mgrScroll()
}

func (a *App) mgrScroll() {
	m := a.mgr
	if m.Cur < m.Scroll {
		m.Scroll = m.Cur
	}
	if m.Cur >= m.Scroll+mgrVisible {
		m.Scroll = m.Cur - mgrVisible + 1
	}
	m.Scroll = max(0, m.Scroll)
}

func (m *Manager) cur() (browser.Entry, bool) {
	if m.Cur < 0 || m.Cur >= len(m.Entries) {
		return browser.Entry{}, false
	}
	return m.Entries[m.Cur], true
}

func (a *App) mgrMove(d int) {
	m := a.mgr
	m.Confirm = false
	a.mgrStopPreview()
	m.Cur = max(0, min(len(m.Entries)-1, m.Cur+d))
	a.mgrScroll()
	a.dirty = true
}

// mgrOpen enters a folder, or loads a file.
func (a *App) mgrOpen() {
	m := a.mgr
	m.Confirm = false
	e, ok := m.cur()
	if !ok {
		return
	}
	if e.Dir {
		a.mgrStopPreview()
		m.Dir, m.Cur, m.Scroll = e.Path, 0, 0
		a.mgrRefresh()
		return
	}
	a.mgrLoad()
}

func (a *App) mgrUp() {
	m := a.mgr
	m.Confirm = false
	a.mgrStopPreview()
	from := filepath.Base(m.Dir)
	m.Dir = a.H.Files.Parent(m.Dir)
	m.Cur, m.Scroll = 0, 0
	a.mgrRefresh()
	for i, e := range m.Entries { // land on the folder we came out of
		if e.Dir && e.Name == from {
			m.Cur = i
		}
	}
	a.mgrScroll()
}

// mgrLoad puts the selected sample in the editor and shows it on Trim.
// The export name gets "-edit" so saving cannot overwrite the original.
func (a *App) mgrLoad() {
	m := a.mgr
	m.Confirm = false
	e, ok := m.cur()
	if !ok || e.Dir {
		a.say("Pick a sample")
		return
	}
	if e.Bad {
		a.say("Not a readable WAV")
		return
	}
	b, err := a.H.Files.Read(e.Path)
	if err != nil {
		a.say("Load: %s", asciiErr(err))
		return
	}
	stem := strings.TrimSuffix(e.Name, filepath.Ext(e.Name))
	a.closeManager()
	a.load(b)
	a.S.Name = stem + "-edit"
	a.Mode, a.Page = ModeTrim, 0
	a.say("Loaded %s", asciiName(stem))
}

// mgrPreview plays or stops the selected sample without loading it.
func (a *App) mgrPreview() {
	m := a.mgr
	m.Confirm = false
	if m.Preview != "" {
		a.mgrStopPreview()
		return
	}
	e, ok := m.cur()
	if !ok || e.Dir || e.Bad {
		a.say("Pick a sample")
		return
	}
	if m.cache.path != e.Path || m.cache.buf == nil {
		b, err := a.H.Files.Read(e.Path)
		if err != nil {
			a.say("Preview: %s", asciiErr(err))
			return
		}
		m.cache.path, m.cache.buf = e.Path, b
	}
	b := m.cache.buf
	if a.Eng.Play(b.Ch, b.Rate, playback.Opts{}) < 0 {
		a.say("Empty sample")
		return
	}
	m.Preview = e.Path
	a.dirty = true
}

func (a *App) mgrStopPreview() {
	if a.mgr == nil || a.mgr.Preview == "" {
		return
	}
	a.Eng.StopAll()
	a.mgr.Preview = ""
	a.dirty = true
}

func (a *App) mgrCopy() {
	m := a.mgr
	m.Confirm = false
	e, ok := m.cur()
	if !ok || e.Dir {
		a.say("Pick a sample to copy")
		return
	}
	m.Clip = e.Path
	a.say("Copied %s", asciiName(e.Name))
}

func (a *App) mgrPaste() {
	m := a.mgr
	m.Confirm = false
	if m.Clip == "" {
		a.say("Nothing copied")
		return
	}
	p, err := a.H.Files.Copy(m.Clip, m.Dir)
	if err != nil {
		a.say("Paste: %s", asciiErr(err))
		return
	}
	a.mgrRefresh()
	for i, e := range m.Entries {
		if e.Path == p {
			m.Cur = i
		}
	}
	a.mgrScroll()
	a.say("Pasted %s", asciiName(filepath.Base(p)))
}

// mgrDelete asks first; the second press deletes.
func (a *App) mgrDelete() {
	m := a.mgr
	e, ok := m.cur()
	if !ok || e.Dir {
		m.Confirm = false
		a.say("Pick a sample to delete")
		return
	}
	if !m.Confirm {
		m.Confirm = true
		a.say("Delete again to confirm")
		return
	}
	m.Confirm = false
	a.mgrStopPreview()
	if err := a.H.Files.Delete(e.Path); err != nil {
		a.say("Delete: %s", asciiErr(err))
		return
	}
	if m.Clip == e.Path {
		m.Clip = ""
	}
	if m.cache.path == e.Path {
		m.cache.path, m.cache.buf = "", nil
	}
	a.mgrRefresh()
	a.say("Deleted %s", asciiName(e.Name))
}

// ---- input while the window is open ------------------------------------------

func (a *App) mgrTop(i int) {
	switch {
	case i <= int(ModeExport):
		a.closeManager()
		a.Mode, a.Page = Mode(i), 0
	case i == 5:
		a.mgrPreview()
	case i == 7:
		if a.H.Leave != nil {
			a.H.Leave()
		}
	}
	a.dirty = true
}

func (a *App) mgrBot(i int) {
	switch i {
	case 0:
		a.mgrLoad()
	case 1:
		a.mgrPreview()
	case 2:
		a.mgrCopy()
	case 3:
		a.mgrPaste()
	case 4:
		a.mgrDelete()
	case 5:
		a.mgrUp()
	case 6:
		a.mgr.Confirm = false
		a.mgrRefresh()
	case 7:
		a.closeManager()
	}
	a.dirty = true
}

func (a *App) mgrButton(cc byte) {
	switch cc {
	case push3.CCDPadUp:
		a.mgrMove(-1)
	case push3.CCDPadDown:
		a.mgrMove(1)
	case push3.CCDPadLeft:
		a.mgrUp()
	case push3.CCDPadRight, push3.CCJogPress, push3.CCDPadCenter:
		a.mgrOpen()
	case push3.CCPlay:
		a.mgrPreview()
	case push3.CCDelete:
		a.mgrDelete()
	case push3.CCDuplicate:
		a.mgrCopy()
	case push3.CCSelect:
		a.mgrPaste()
	default:
		a.mgr.Confirm = false
	}
	a.dirty = true
}

func (a *App) mgrEncoder(cc byte, d int) {
	if cc == push3.CCJogWheel || cc == push3.CCEncoder1 {
		a.mgrMove(d)
	}
}

// ---- text helpers --------------------------------------------------------------

// asciiName makes a file name safe for the screen font (no glyph past ASCII).
func asciiName(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r < 32 || r > 126 {
			r = '?'
		}
		out = append(out, r)
	}
	return string(out)
}

func tail(s string, n int) string {
	s = asciiName(s)
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n+3:]
}

// ---- what the screen shows -------------------------------------------------------

func detail(e browser.Entry) string {
	switch {
	case e.Dir:
		return "folder"
	case e.Bad:
		return "unreadable"
	}
	ch := "M"
	if e.Info.Channels > 1 {
		ch = "S"
	}
	return sprintf("%.1fs %dk %s %s", e.Seconds(), e.Info.Rate/1000, ch, size(e.Size))
}

func size(n int64) string {
	switch {
	case n >= 10<<20:
		return sprintf("%dMB", n>>20)
	case n >= 1<<20:
		return sprintf("%.1fMB", float64(n)/(1<<20))
	}
	return sprintf("%dKB", max(n>>10, 1))
}

func (a *App) mgrFrame() *MgrFrame {
	m := a.mgr
	f := &MgrFrame{Dir: tail(m.Dir, 70), Total: len(m.Entries), Confirm: m.Confirm}
	end := min(m.Scroll+mgrVisible, len(m.Entries))
	for i := m.Scroll; i < end; i++ {
		e := m.Entries[i]
		f.Rows = append(f.Rows, MgrRow{Name: asciiName(e.Name), Detail: detail(e), Dir: e.Dir, Bad: e.Bad,
			Marked: m.Clip != "" && e.Path == m.Clip, Playing: m.Preview == e.Path})
	}
	f.Cursor = m.Cur - m.Scroll
	f.Index = m.Cur + 1
	f.Scroll = m.Scroll > 0 || end < len(m.Entries)
	return f
}

func (a *App) mgrLabels(f *Frame) {
	m := a.mgr
	p := &f.Params
	p[0] = Param{"Folder", tail(filepath.Base(m.Dir), 16)}
	if e, ok := m.cur(); ok {
		p[1] = Param{"Item", sprintf("%d/%d", m.Cur+1, len(m.Entries))}
		if !e.Dir && !e.Bad {
			p[2] = Param{"Length", sprintf("%.2fs", e.Seconds())}
			ch := "Mono"
			if e.Info.Channels > 1 {
				ch = "Stereo"
			}
			p[3] = Param{"Format", sprintf("%d-bit %s", e.Info.Bits, ch)}
			p[4] = Param{"Rate", sprintf("%d Hz", e.Info.Rate)}
		}
	}
	if m.Clip != "" {
		p[6] = Param{"Clipboard", tail(filepath.Base(m.Clip), 16)}
	}
	del := Key{Label: "Delete"}
	if m.Confirm {
		del = Key{Label: "Sure?", On: true}
	}
	_, ok := m.cur()
	f.Bot = [8]Key{
		{Label: "Load", Off: !ok},
		{Label: map[bool]string{false: "Preview", true: "Stop"}[m.Preview != ""], On: m.Preview != ""},
		{Label: "Copy", Off: !ok},
		{Label: "Paste", Off: m.Clip == ""},
		del,
		{Label: "Up"},
		{Label: "Refresh"},
		{Label: "Back"},
	}
	f.Info = "SAMPLES"
}

func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }
