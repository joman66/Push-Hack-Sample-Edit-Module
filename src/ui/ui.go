// Package ui draws SampleEdit on Push's 960x160 screen. Every string is
// ASCII: the screen font has no other glyphs (see CLAUDE.md).
package ui

import (
	"fmt"
	"image"
	"image/color"

	"github.com/federico-pepe/ableton-push-hack/core/gfx"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/text"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/widgets"
	"github.com/federico-pepe/ableton-push-hack/core/push3"
	"github.com/federico-pepe/push-hack-sample-edit/src/app"
)

// Screen size and layout, in pixels.
const (
	W, H   = 960, 160
	colW   = W / 8
	topH   = 16
	waveY  = topH
	waveH  = 78
	paramY = waveY + waveH
	botY   = 142
	botH   = H - botY
)

func pal(name string) color.NRGBA {
	i, _ := push3.ColorByName(name)
	return push3.ColorForIndex(i).RGB
}

var (
	theme   = widgets.Default
	cWave   = pal("sky")
	cSel    = color.NRGBA{R: 40, G: 70, B: 140, A: 255}
	cSlice  = pal("amber")
	cCur    = pal("white")
	cHead   = pal("green_vv")
	cRec    = pal("red")
	cDim    = pal("gray_mid")
	cOnKey  = pal("white")
	cOffKey = pal("dgray")
)

// New returns a blank frame.
func New() *image.NRGBA { return image.NewNRGBA(image.Rect(0, 0, W, H)) }

// Draw paints one frame.
func Draw(img *image.NRGBA, f app.Frame) {
	gfx.FillRect(img, 0, 0, W, H, theme.Black)
	drawTop(img, f)
	if f.Mgr != nil {
		drawManager(img, f.Mgr)
	} else if f.Mode == app.ModeRecord {
		drawRecord(img, f)
	} else {
		drawWave(img, f)
	}
	drawParams(img, f)
	var btns [8]widgets.SoftButton
	for i, k := range f.Bot {
		st := widgets.SoftNeutral
		switch {
		case k.Off:
			st = widgets.SoftOff
		case k.On:
			st = widgets.SoftOn
		}
		btns[i] = widgets.SoftButton{Label: text.Truncate(k.Label, 8), State: st}
	}
	widgets.DrawBotStrip(img, theme, botY, W, colW, botH, btns, "")
	if f.Status != "" {
		drawStatus(img, f.Status)
	}
}

func drawTop(img *image.NRGBA, f app.Frame) {
	gfx.FillRect(img, 0, 0, W, topH, theme.DarkGray)
	for i, k := range f.Top {
		c := cDim
		if k.On {
			c = cOnKey
			gfx.FillRect(img, i*colW+2, topH-3, colW-4, 2, cHead)
		}
		text.Draw(img, i*colW+6, 12, text.Truncate(k.Label, 8), c)
	}
	if f.Shift {
		text.Draw(img, W-50, 12, "SHIFT", cSlice)
	}
}

func xOf(f app.Frame, frame int) int {
	if f.ViewSpan == 0 {
		return 0
	}
	return (frame - f.ViewStart) * W / f.ViewSpan
}

func drawWave(img *image.NRGBA, f app.Frame) {
	mid := waveY + waveH/2
	if f.Frames == 0 {
		text.Draw(img, 330, mid, "No sample - press Record", cDim)
		return
	}
	if f.Sel.Len() > 0 {
		x0 := max(0, xOf(f, f.Sel.Start))
		x1 := min(W, xOf(f, f.Sel.End))
		if x1 > x0 {
			gfx.FillRect(img, x0, waveY, x1-x0, waveH, cSel)
		}
	}
	for x, p := range f.Peaks {
		if p[0] > p[1] {
			continue // column past the end of the sample
		}
		y0 := mid - int(p[1]*float32(waveH/2-1))
		y1 := mid - int(p[0]*float32(waveH/2-1))
		if y1 <= y0 {
			y1 = y0 + 1
		}
		gfx.FillRect(img, x, y0, 1, y1-y0, cWave)
	}
	for i, s := range f.Slices {
		x := xOf(f, s[0])
		if x < 0 || x >= W {
			continue
		}
		c := cSlice
		if i == f.Cur {
			c = cCur
		}
		gfx.FillRect(img, x, waveY, 1, waveH, c)
		text.Draw(img, x+2, waveY+10, fmt.Sprint(i+1), c)
	}
	if f.Playhead >= 0 {
		if x := xOf(f, f.Playhead); x >= 0 && x < W {
			gfx.FillRect(img, x, waveY, 2, waveH, cHead)
		}
	}
}

func drawRecord(img *image.NRGBA, f app.Frame) {
	mid := waveY + waveH/2
	label := map[int]string{0: "READY", 1: "ARMED", 2: "RECORDING"}[int(f.RecState)]
	c := cDim
	if f.RecState == 2 {
		c = cRec
	}
	text.DrawScaled(img, 20, waveY+34, 3, label, c)
	// level meter
	lv := f.RecLevel
	if lv > 1 {
		lv = 1
	}
	widgets.DrawMeter(img, 20, waveY+50, 700, 14, lv, pal("green_vv"), theme.DarkGray)
	if f.Frames > 0 {
		text.Draw(img, 740, mid-8, fmt.Sprintf("Last take %s s", sec(f.Frames, f.Rate)), cDim)
	}
}

func sec(frames, rate int) string {
	if rate == 0 {
		return "0.00"
	}
	return fmt.Sprintf("%.2f", float64(frames)/float64(rate))
}

func drawParams(img *image.NRGBA, f app.Frame) {
	gfx.FillRect(img, 0, paramY, W, botY-paramY, theme.CrumbBg)
	for i, p := range f.Params {
		if p.Name == "" {
			continue
		}
		x := i*colW + 6
		text.Draw(img, x, paramY+14, text.Truncate(p.Name, 16), cDim)
		text.Draw(img, x, paramY+30, text.Truncate(p.Value, 16), theme.White)
	}
	if f.Info != "" {
		text.Draw(img, W-8-text.Width(f.Info), paramY+46, f.Info, cSlice)
	}
}

func drawStatus(img *image.NRGBA, s string) {
	s = text.Truncate(s, 60)
	w := text.Width(s) + 16
	x := (W - w) / 2
	gfx.FillRect(img, x, waveY+waveH/2-12, w, 24, theme.StatusBg)
	text.Draw(img, x+8, waveY+waveH/2+4, s, theme.StatusCol)
}

// drawManager paints the Sample Manager list in the waveform area.
func drawManager(img *image.NRGBA, m *app.MgrFrame) {
	const rowH = 13
	gfx.FillRect(img, 0, waveY, W, rowH, theme.CrumbBg)
	text.Draw(img, 6, waveY+10, text.Truncate(m.Dir, 80), theme.CrumbCol)
	pos := fmt.Sprintf("%d/%d", m.Index, m.Total)
	text.Draw(img, W-8-text.Width(pos), waveY+10, pos, theme.CrumbCol)
	if len(m.Rows) == 0 {
		text.Draw(img, 6, waveY+rowH+14, "Empty folder - no WAV files here", cDim)
		return
	}
	for i, r := range m.Rows {
		y := waveY + rowH*(i+1)
		c := theme.White
		switch {
		case r.Bad:
			c = theme.OffColor
		case r.Dir:
			c = theme.DirColor
		}
		if i == m.Cursor {
			bg := theme.Select
			if m.Confirm {
				bg = theme.Accent
			}
			gfx.FillRect(img, 0, y, W-6, rowH, bg)
		}
		mark := "  "
		switch {
		case r.Playing:
			mark = "> "
		case r.Marked:
			mark = "* "
		}
		name := r.Name
		if r.Dir {
			name = "[" + name + "]"
		}
		text.Draw(img, 6, y+10, text.Truncate(mark+name, 52), c)
		text.Draw(img, W-14-text.Width(r.Detail), y+10, r.Detail, c)
	}
	if m.Scroll && m.Total > 0 {
		h := waveH - rowH
		bar := max(h*len(m.Rows)/m.Total, 6)
		top := waveY + rowH + (h-bar)*(m.Index-1)/max(m.Total-1, 1)
		gfx.FillRect(img, W-4, top, 3, bar, cDim)
	}
}
