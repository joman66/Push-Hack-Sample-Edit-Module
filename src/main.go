// SampleEdit: an offline sampler and editor for Push 3 Standalone. It is an
// app of Push Hack Home (hack.json "surface"): it draws and reads presses
// only while it has the surface.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
	"github.com/federico-pepe/ableton-push-hack/core/audiobus"
	"github.com/federico-pepe/ableton-push-hack/core/hackcfg"
	"github.com/federico-pepe/ableton-push-hack/core/httpx"
	"github.com/federico-pepe/ableton-push-hack/core/pmclient"
	"github.com/federico-pepe/ableton-push-hack/core/push3"
	"github.com/federico-pepe/push-hack-sample-edit/src/app"
	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
	"github.com/federico-pepe/push-hack-sample-edit/src/autosample"
	"github.com/federico-pepe/push-hack-sample-edit/src/library"
	"github.com/federico-pepe/push-hack-sample-edit/src/monitor"
	"github.com/federico-pepe/push-hack-sample-edit/src/playback"
	"github.com/federico-pepe/push-hack-sample-edit/src/record"
	"github.com/federico-pepe/push-hack-sample-edit/src/route"
	"github.com/federico-pepe/push-hack-sample-edit/src/ui"
)

const pmBase = "http://127.0.0.1:7701"

// settings is the extra part of hack.json this hack reads.
type settings struct {
	Settings struct {
		Library *library.Dirs     `json:"library"`
		Inputs  map[string]string `json:"inputs"`   // name -> ALSA capture device
		MidiOut string            `json:"midi_out"` // part of the port name
		Rate    int               `json:"rate"`
		KeepXML bool              `json:"keep_xml"`
	} `json:"settings"`
}

func main() {
	cfgPath := flag.String("config", "hack.json", "path to hack.json")
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("sample-edit: ")

	cfg, err := hackcfg.Load(*cfgPath, 7711)
	if err != nil {
		log.Fatal(err)
	}
	var ex settings
	if raw, err := os.ReadFile(*cfgPath); err == nil {
		json.Unmarshal(raw, &ex)
	}
	s := ex.Settings
	dirs := library.DefaultDirs()
	if s.Library != nil {
		d := *s.Library
		if d.Samples != "" {
			dirs.Samples = d.Samples
		}
		if d.Simpler != "" {
			dirs.Simpler = d.Simpler
		}
		if d.Sampler != "" {
			dirs.Sampler = d.Sampler
		}
		if d.Drum != "" {
			dirs.Drum = d.Drum
		}
	}
	dirs.KeepXML = s.KeepXML
	if s.Rate == 0 {
		s.Rate = 48000
	}
	if s.MidiOut == "" {
		s.MidiOut = "External Port"
	}
	inputs, inputNames := inputList(s.Inputs)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pm := pmclient.New(pmBase)
	pm.App = cfg.ID

	rec := record.New(record.Config{Rate: s.Rate, Channels: 2, TmpDir: filepath.Join(dirs.Samples, "_rec")})
	eng := playback.New(48000)
	mon := monitor.New(s.Rate)
	var busUp atomic.Bool // the audio bus gave us a format, so audio out works
	src := &sourceCtl{rec: rec, mon: mon, rate: s.Rate, inputs: inputs, names: inputNames}

	var out *midiOut
	var outMu sync.Mutex
	getOut := func() *midiOut {
		outMu.Lock()
		defer outMu.Unlock()
		if out == nil {
			o, err := openMidiOut(s.MidiOut)
			if err != nil {
				log.Printf("midi out: %v", err)
				return nil
			}
			out = o
		}
		return out
	}

	var a *app.App
	hooks := app.Hooks{
		LiveTempo:   func() float64 { t, _ := pm.Tempo(); return t },
		Leave:       func() { pm.SurfaceLeave("live") },
		SourceNames: inputNames,
		SourceOn:    func(i int) error { return src.on(ctx, i) },
		SourceOff:   src.off,
		Monitor: func(on bool, i int) error {
			if on && !busUp.Load() {
				return fmt.Errorf("audio bus not running")
			}
			return src.setMonitor(ctx, on, i, rec.State() == record.Idle)
		},
		Save: &library.Library{D: dirs},
		AutoSample: func(c context.Context, cfg autosample.Config, p autosample.Progress) ([]autosample.Zone, error) {
			o := getOut()
			if o == nil {
				return nil, fmt.Errorf("no MIDI out port %q", s.MidiOut)
			}
			if err := src.on(c, a.SourceIndex()); err != nil {
				return nil, err
			}
			defer src.off()
			return autosample.Run(c, cfg, &synth{out: o, rec: rec}, p)
		},
	}
	a = app.New(hooks, rec, eng)

	var focused atomic.Bool
	var focusSeq atomic.Int64
	go watchDeps(ctx, pm)
	go func() {
		err := pm.RunSurface(ctx, nil, pmclient.SurfaceHandler{
			OnFocus: func() { focused.Store(true); focusSeq.Add(1) },
			OnBlur:  func() { focused.Store(false); eng.StopAll(); a.MonitorStop() },
		})
		if err != nil && ctx.Err() == nil {
			log.Printf("surface: %v (needs push-manager with Push Hack Home)", err)
		}
	}()

	go audioLoop(ctx, eng, mon, &busUp)
	go midiLoop(ctx, a, &focused)
	go renderLoop(ctx, a, pm, &focused, &focusSeq)
	go serve(ctx, cfg, a)

	<-ctx.Done()
	src.off()
	log.Print("stopped")
}

func inputList(m map[string]string) (map[string]string, []string) {
	if len(m) == 0 {
		m = map[string]string{"Line": "hw:0,0", "ADAT": "hw:0,1", "Loop": "hw:PHVAudio,1,0", "Routed": "ph_in_sample-edit_in"}
	}
	order := []string{"Line", "ADAT", "Loop"}
	var names []string
	for _, n := range order {
		if _, ok := m[n]; ok {
			names = append(names, n)
		}
	}
	for n := range m {
		found := false
		for _, x := range names {
			found = found || x == n
		}
		if !found {
			names = append(names, n)
		}
	}
	return m, names
}

// ---- capture source -------------------------------------------------------

type sourceCtl struct {
	mu      sync.Mutex
	rec     *record.Recorder
	mon     *monitor.Monitor
	rate    int
	inputs  map[string]string
	names   []string
	cancel  context.CancelFunc
	cur     record.Source
	curIdx  int
	monitor bool // the monitor wants the capture to keep running
}

// on starts capturing input i. A capture that already runs on another input
// (only the monitor leaves one running) is restarted on the new one.
func (s *sourceCtl) on(parent context.Context, i int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.onLocked(parent, i)
}

func (s *sourceCtl) onLocked(parent context.Context, i int) error {
	if len(s.names) == 0 {
		return fmt.Errorf("no inputs configured")
	}
	i %= len(s.names)
	if s.cancel != nil {
		if s.curIdx == i {
			return nil
		}
		s.stopLocked()
	}
	dev := s.inputs[s.names[i]]
	if err := route.Check(dev); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	src, err := record.OpenArecordEnv(ctx, dev, s.rate, 2, route.Env(dev))
	if err != nil {
		cancel()
		return err
	}
	s.cancel, s.cur, s.curIdx = cancel, src, i
	go func() {
		err := record.PumpTap(ctx, src, 2, 480, s.rec, s.mon.Push)
		if ctx.Err() == nil {
			log.Printf("capture ended: %v", err)
		}
	}()
	return nil
}

func (s *sourceCtl) stopLocked() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.cur != nil {
		go s.cur.Close()
		s.cur = nil
	}
}

// off stops capturing, unless the monitor still needs the input.
func (s *sourceCtl) off() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.monitor {
		return
	}
	s.stopLocked()
}

// setMonitor turns the live monitor on input i, or off. It keeps the capture
// running while the monitor is on. It stops the capture when the monitor goes
// off, unless the recorder is armed or recording (idle tells that).
func (s *sourceCtl) setMonitor(parent context.Context, on bool, i int, idle bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if on {
		if err := s.onLocked(parent, i); err != nil {
			return err
		}
		s.monitor = true
		s.mon.SetOn(true)
		return nil
	}
	s.monitor = false
	s.mon.SetOn(false)
	if idle {
		s.stopLocked()
	}
	return nil
}

// ---- auto-sampler player ----------------------------------------------------

type midiOut struct {
	c   *alsaseq.Client
	dst alsaseq.Addr
}

func openMidiOut(name string) (*midiOut, error) {
	alsaseq.WaitForBootSettle()
	p, ok := alsaseq.FindByName(name, alsaseq.CapWrite|alsaseq.CapSubsWrite)
	if !ok {
		return nil, fmt.Errorf("no writable MIDI port named %q", name)
	}
	c, err := alsaseq.Open()
	if err != nil {
		return nil, err
	}
	if _, err := c.CreatePort("SampleEdit Out", alsaseq.CapRead|alsaseq.CapSubsRead, alsaseq.PortTypeMidi|alsaseq.PortTypeApp); err != nil {
		c.Close()
		return nil, err
	}
	c.SetName("SampleEdit")
	return &midiOut{c: c, dst: p.Addr}, nil
}

func (o *midiOut) note(ch, n, v int) {
	o.c.SendNote(o.dst, byte(ch), byte(n), byte(v))
}

// synth plays one note on the external synth and records it.
type synth struct {
	out *midiOut
	rec *record.Recorder
}

func sleep(ctx context.Context, sec float64) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(sec * float64(time.Second))):
		return nil
	}
}

func (s *synth) Play(ctx context.Context, ch, note, vel int, hold, rel, pre float64) (autosample.Take, error) {
	s.rec.SetConfig(record.Config{PreRollSec: pre, ThresholdDB: -90})
	s.rec.Arm(false)
	if err := sleep(ctx, pre+0.05); err != nil {
		s.rec.Cancel()
		return autosample.Take{}, err
	}
	if err := s.rec.Start(); err != nil {
		return autosample.Take{}, err
	}
	on := s.rec.Frames()
	s.out.note(ch, note, vel)
	err := sleep(ctx, hold)
	off := s.rec.Frames()
	s.out.note(ch, note, 0)
	if err == nil {
		err = sleep(ctx, rel)
	}
	if err != nil {
		s.rec.Cancel()
		return autosample.Take{}, err
	}
	b, err := s.rec.Stop()
	if err != nil {
		return autosample.Take{}, err
	}
	return autosample.Take{Audio: b, On: on, Off: off}, nil
}

// ---- audio out (preview) ------------------------------------------------------

func audioLoop(ctx context.Context, eng *playback.Engine, mon *monitor.Monitor, busUp *atomic.Bool) {
	cl := audiobus.Open(audiobus.SocketPath(audiobus.DefaultDir), "SampleEdit", -1)
	defer cl.Close()
	var gen uint64 = ^uint64(0)
	var buf []float32
	rate := 0
	for ctx.Err() == nil {
		if g := cl.Generation(); g != gen {
			gen = g
			f := cl.Format()
			if f.Rate > 0 && f.Period > 0 {
				eng.SetRate(f.Rate)
				rate = f.Rate
				buf = make([]float32, f.Period*2)
				busUp.Store(true)
			}
		}
		if buf == nil {
			time.Sleep(200 * time.Millisecond)
			if f := cl.Format(); f.Rate > 0 {
				gen = ^uint64(0)
			}
			continue
		}
		eng.Render(buf)
		mon.Render(buf, rate) // adds the live input when the monitor is on
		cl.Write(buf)
	}
}

// ---- MIDI in (Push) -------------------------------------------------------------

type handler struct {
	a  *app.App
	p3 *alsaseq.Push3
	on *atomic.Bool
}

func (h *handler) Fixed(ev uint8, src alsaseq.Addr, d []byte) {
	if !h.on.Load() || !h.p3.Is(src) {
		return
	}
	switch ev {
	case alsaseq.EvController:
		cc := byte(binary.LittleEndian.Uint32(d[4:]))
		h.a.CC(cc, byte(binary.LittleEndian.Uint32(d[8:])))
	case alsaseq.EvNoteOn:
		h.a.Pad(d[1], d[2])
	}
}
func (h *handler) VarLen(uint8, alsaseq.Addr, []byte) {}

func midiLoop(ctx context.Context, a *app.App, focused *atomic.Bool) {
	alsaseq.WaitForBootSettle()
	var p3 alsaseq.Push3
	for ctx.Err() == nil {
		c, err := alsaseq.Open()
		if err == nil {
			_, err = c.CreatePort("SampleEdit In", alsaseq.CapWrite|alsaseq.CapSubsWrite, alsaseq.PortTypeMidi|alsaseq.PortTypeApp)
		}
		if err != nil {
			log.Printf("midi in: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		c.SetName("SampleEdit")
		p3.Refresh()
		if err := c.Subscribe(p3.Addr()); err != nil {
			log.Printf("subscribe Push 3: %v", err)
		}
		a.SetLED(&ledSink{c: c, p3: &p3})
		go func() { <-ctx.Done(); c.Close() }()
		c.ReadLoop(&handler{a: a, p3: &p3, on: focused})
		c.Close()
		time.Sleep(time.Second)
	}
}

type ledSink struct {
	c  *alsaseq.Client
	p3 *alsaseq.Push3
}

func (l *ledSink) CC(cc, v byte) { l.c.SendCC(l.p3.Addr(), 0, cc, int32(v)) }
func (l *ledSink) Pad(n, v byte) { l.c.SendNote(l.p3.Addr(), 0, n, v) }

// ---- screen and LEDs ---------------------------------------------------------------

func renderLoop(ctx context.Context, a *app.App, pm *pmclient.Client, focused *atomic.Bool, seq *atomic.Int64) {
	t := time.NewTicker(40 * time.Millisecond)
	defer t.Stop()
	img := ui.New()
	var seen int64
	var lastStatus time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !focused.Load() {
			continue
		}
		force := false
		if s := seq.Load(); s != seen {
			seen, force = s, true
			a.ResetLEDs()
		}
		dirty := a.Tick()
		a.PaintLEDs()
		if !dirty && !force {
			continue
		}
		f := a.Snapshot(ui.W)
		ui.Draw(img, f)
		if err := pm.Push(img); err != nil {
			log.Printf("display: %v", err)
		}
		if time.Since(lastStatus) > 600*time.Millisecond {
			lastStatus = time.Now()
			pm.SurfaceStatus(f.Mode.String(), fmt.Sprintf("%.1fs %s", float64(f.Frames)/float64(max(f.Rate, 1)), f.RecState))
		}
	}
}

// watchDeps logs when push-manager or push-display goes away or comes back.
func watchDeps(ctx context.Context, pm *pmclient.Client) {
	state := ""
	for ctx.Err() == nil {
		st, err := pm.DisplayStatus()
		now := "ok"
		switch {
		case err != nil:
			now = "push-manager unreachable"
		case !st.Connected:
			now = "push-manager up, push-display framebuffer not connected"
		}
		if now != state {
			state = now
			if now == "ok" {
				log.Print("display dependency ok")
			} else {
				log.Printf("WARNING: %s (SampleEdit needs push-manager and push-display)", now)
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
		}
	}
}

// ---- HTTP status ------------------------------------------------------------------------

func serve(ctx context.Context, cfg hackcfg.Config, a *app.App) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		f := a.Snapshot(1)
		httpx.JSON(w, map[string]any{
			"id": cfg.ID, "version": cfg.Version, "mode": f.Mode.String(),
			"frames": f.Frames, "rate": f.Rate, "slices": len(f.Slices), "recorder": f.RecState.String(),
		})
	})
	srv := httpx.NewServer(fmt.Sprintf("127.0.0.1:%d", cfg.Port), httpx.WithLogging(mux))
	go func() { <-ctx.Done(); srv.Close() }()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("http: %v", err)
	}
}

var _ = push3.CCShift
var _ = audio.MinDB
