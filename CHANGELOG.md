# Changelog

## [Unreleased]

### Changed
- The input comes from push-audio-loopback. Sample Edit has one stereo input, **Sample in**, that you connect in the Link inputs matrix of the loopback web page. Needs `core` v0.5.5 and the loopback module in Link mode.
- Record page: the Input key is now the Monitor key. Takes use the rate of the input (44.1 kHz).

### Removed
- The Line, ADAT, Loop and Routed inputs, the `arecord` capture, the `inputs` setting, and the `audio` block in `hack.json` (the old Push Manager audio routing matrix).

### Added
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
