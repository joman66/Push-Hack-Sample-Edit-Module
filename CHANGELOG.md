# Changelog

## [Unreleased]

### Fixed
- Simpler, Sampler and Drum Rack presets now load in Live 12.4.6 on a Push 3. They are filled in from presets that a real Live saved, instead of a hand-written XML. The Sampler preset plays the samples as they are (no factory sound). Drum Rack pads are Simpler in One-Shot mode.

### Changed
- The input comes from push-audio-loopback. Sample Edit has one stereo input, **Sample in**, that you connect in the Link inputs matrix of the loopback web page. Needs `core` v0.5.5 and the loopback module in Link mode.
- Record page: the Input key is now the Monitor key. Takes use the rate of the input (44.1 kHz).

### Removed
- The Line, ADAT, Loop and Routed inputs, the `arecord` capture, the `inputs` setting, and the `audio` block in `hack.json` (the old Push Manager audio routing matrix).

### Added
- Chop: **Duplicate** adds a slice point at the selected slice, so you can move it by hand with SlcSta.
- Chop: the **Sens** knob re-slices live in Transient mode.
- Chop: moving a shared slice point (SlcSta, SlcEnd) moves the slice next to it too.
- Live monitor of the input (Monitor key, Enc 3).
- Sample Manager window (Record > Load): browse the sample folders on the device, load, preview,
  copy, paste and delete (with confirm) WAV files. Replaces Load reloading only the last export.
- First version of Sample Edit, an app of Push Hack Home.
- Record page: pre-roll, threshold auto-start, mono or stereo, direct-to-disk take.
- Auto-sampler: note stride, 1 to 8 velocity layers, hold and release, loop point search.
- Trim page: crop, silence, auto-trim, copy, cut, paste over, paste insert, zero-crossing snap.
- Chop page: transient, grid, equal and manual slicing, 16-velocity pads, choke groups.
- Process page: normalize (peak or RMS), gain, fades, reverse, pitch shift, time stretch.
- Export page: 24-bit 48 kHz WAV with slice and loop tags, Simpler, Sampler and Drum Rack presets.
- Multi-step undo and redo.

### Known issues
- The `.adv` and `.adg` presets have not been opened in a real Live yet.
- The Link input has not been tried on a Push yet.
- Presets are written with `OriginalCrc` 0. Live finds a moved sample by its path, so this should not matter, but it has not been checked on a Push.
