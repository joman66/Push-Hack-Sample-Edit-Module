# Sample Edit

An offline sampler and editor for the Ableton Push 3 Standalone. It is an app of
[Push Hack Home](https://github.com/federico-pepe/ableton-push-hack): open Home with
Shift + Help and pick **SAMPLE**. Help goes back to Live.

**It needs push-manager and push-display installed and running.** It draws through
push-manager's display API. The log tells you if one of them is missing.
For preview sound and for the input, also install **push-audio-loopback** and leave it in
**Link** mode (its default).

## Controls

Upper soft-keys: **Record, Trim, Chop, Process, Export**, then **Play**, **Loop**, **LIVE**.
Lower soft-keys change with the page. Turn an encoder to move fast. Hold **Shift** while you
turn for sample-accurate steps.

| Control | Does |
|---|---|
| Enc 1 / Enc 2 | Zoom / scroll the waveform |
| Enc 3 / Enc 4 | Selection start / end |
| Shift + Duplicate | Copy |
| Shift + Delete | Cut (closes the gap) |
| Select | Paste over, at the playhead |
| Shift + Select | Paste insert, at the playhead |
| Undo / Shift + Undo | Undo / redo |
| Delete (Chop page) | Remove the slice |
| Page left / right | Second page of Record and Process |

### Pages
- **Record**: Arm, Cancel, AutoGo (start on threshold), Monitor, Mono/Stereo,
  Auto> (auto-sampler), **Load** (opens the Sample Manager), Clear. Enc 1 threshold, Enc 2 pre-roll.
  **Input.** Sample Edit has one stereo input, **Sample in**. You choose its source in the
  **Link inputs** matrix on the web page of push-audio-loopback (Push Hack menu, Web UI): click
  the dot where a Link channel (a Live track, a return, Main, or the output of another hack)
  meets the column **SampleEdit / Sample in**. The route is saved there and stays after a
  restart. With nothing connected, the take is empty. The input runs at 44.1 kHz, so takes are
  44.1 kHz too.
  **Monitor** plays the live input on the audio bus so you hear what it picks up. Turn it
  with the Monitor key (lower key 4), with Enc 3 (right is on, left is off), or with the Monitor key
  on the second Record page. It switches off when Live gets the surface back. It adds
  about one audio period of delay plus the input's own. Take care with feedback: do not route
  Live's output back into this input while monitoring.
  *Auto-sampler page*: Low, High, Stride, Layers (1 to 8), Hold, Release, MIDI channel.
  It plays each note on the Push's MIDI out, records it, finds a sustain loop, and keeps the zones.
- **Sample Manager** (Record > **Load**): a window for the WAV files stored on the Push. It lists the
  folders and samples in Live's User Library sample folder, with length, rate and size.

  | Control | Does |
  |---|---|
  | D-pad up / down, Enc 1, jog wheel | Move in the list |
  | D-pad right, jog press | Open a folder (or load a sample) |
  | D-pad left, **Up** | Go up a folder (never above the top folder) |
  | **Load** | Put the sample in the editor and show it on Trim |
  | **Preview** (also Play or any pad) | Hear it without loading. Press again to stop |
  | **Copy** (Shift + Duplicate) | Mark the sample |
  | **Paste** (Select) | Copy the marked file into the open folder, as `name copy.wav` if the name is taken |
  | **Delete** (Delete button) | Press once to ask, press again to delete. Anything else cancels |
  | **Refresh**, **Back** | Read the folder again, close the window |

  A loaded sample is exported as `<name>-edit`, so saving never overwrites the original.
  Only `.wav` files can be deleted, never folders, and nothing outside the top folder can be touched.
  Presets that point at a deleted WAV will not find it. Set the top folder with `settings.library.browse`.
- **Trim**: Crop, Silence, AutoTrm, Reverse, Copy, Cut, Paste, Insert. **Shift + lower key 1**
  snaps the selection to zero crossings.
- **Chop**: Transient, Grid (1/8, 1/16, 1/32 at Live's tempo or the detected one), Equal (4 to 64),
  Manual (tap pads while it plays), Clear, 16 Vel, Nudge (zero crossing), Assign (choke group).
  Pads light for each slice. Tap a pad to hear and select it. Enc 7 / Enc 8 (SlcSta, SlcEnd) move the slice ends.
  When two slices share a point, moving it moves both, so there is no gap or overlap.
  **Sens** (Enc 5): in Transient mode the slices follow the knob as you turn it (about 3 ms for 30 s of audio).
  It does not touch Grid, Equal or Manual slices, so hand edits are safe there. Pressing Transnt again also
  throws away edits made to transient slices.
  **Duplicate** adds a slice point at the selected slice. The new point starts 10 ms after the selected
  one and becomes the selected slice. Turn Enc 7 (SlcSta) to move it. **Delete** removes a point (the slice
  joins the one before it).
- **Process**: Normal (peak or RMS), Gain (-inf to +24 dB), FadeIn, FadeOut (Lin, Exp, Log), Reverse.
  Page 2: Pitch (+/- 24 st), Stretch (x0.25 to x4), live or destructive pitch, Rhythm or Tonal.
- **Export**: Simpler, Drum Rk, WAV, Sampler, All. Enc 5 root key, Enc 6 loop crossfade,
  Enc 7 live pitch. Name+/Name- changes the file name.

Files go to Live's User Library: WAV in `Samples/Imported/SampleEdit`, presets under
`Presets/Instruments/{Simpler,Sampler,Drum Rack}/SampleEdit`. Change them in `hack.json` `settings.library`.
WAVs are 24-bit, at the rate of the take, with loop (`smpl`), slice regions (`cue`/`adtl`) and tempo (`acid`) tags.

## Limits
- Mono and stereo only. At most 64 slices (one per pad).
- Pitch and stretch are offline and can take a few seconds on a long sample.
- The screen font is ASCII only.

## Build
```
make test
make          # builds ./sample-edit for Push (linux/amd64, static)
```
Install it through Push Hack Catalog, or copy `sample-edit` and `hack.json` to
`/data/push-hack/hacks/sample-edit/` and run it over SSH.

## Not verified on a device yet
The Link input on a Push (the capture is tested off-device against a real audio bus server),
the monitor (level, delay), the presets in Live, and the MIDI out port.
See `plans/2026-11-03-sample-edit-module.md`.
