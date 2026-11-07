# Sample Edit module

Date: 2026-11-03

## Goal
An MPC-style sampler and editor that runs on a Push 3 Standalone with no Live session.

## Decisions
- Own repo (`push-hack-sample-edit`), not in `hacks/`, as `docs/adding-a-hack.md` says.
- An app of Push Hack Home (`surface` in `hack.json`, `pmclient.RunSurface`). No button chord.
  Help is left to push-manager.
- Screen only through `pmclient.Push`. LEDs and presses through our own ALSA seq clients.
- Every `/dev/snd` open sits behind `alsaseq.WaitForBootSettle()`.
- Preview audio goes through `core/audiobus`, not the loopback card.
- Undo stores only the changed range of each edit (replace-range steps), capped at 96 MB.
- Recording reads the one stereo input `Sample in` of `core/audiobus` (`OpenWithInputs`, `Client.Read`), which the loopback module's Link inputs matrix fills. No cgo, no arecord. It reads only while armed or monitoring. (Changed 2026-11-07; before that it used `arecord` with Line, ADAT and Loop.)
- Time stretch is WSOLA. Pitch shift is stretch plus resample.

## Sample Manager (2026-11-04)
- Own package `src/browser` (list, read, copy, delete) and own window in `src/app/manager.go`.
- Every path is resolved and must stay inside the browse root. Delete takes only `.wav` files.
- Copy never overwrites (`O_EXCL`, `name copy N.wav`). Delete needs two presses.
- Listing probes WAV headers only, so a big folder does not load audio.
- A loaded file gets the export name `<name>-edit` so an export cannot overwrite it.
- Preview plays through the same engine and audio bus as the editor.

## Presets from real Live files (2026-11-07)
- The first exporter wrote XML from memory and Live did not load it. Live needs every node of a real preset.
- `src/export/tmpl/*.xml` are cut from a Simpler, a Sampler (multi zone) and a Drum Rack that Live 12.4.6 saved on a Push 3.
  `scripts/make_preset_templates.py` rebuilds them and leaves `{{PLACEHOLDERS}}` for the parts that change.
- The Sampler reference is a factory preset, so the script resets its sound (filter, pitch envelope, sub oscillator, transpose 12, key zone shift) to the plain Simpler values.
- Live stores the last frame in `SampleEnd` (frames - 1), a pad's `ReceivingNote` as 128 minus the note, and a sample in the User Library as path type 6 with a relative path.
- Checked: a Simpler made with the values of the reference matches the real file except the CRC and slice points. A Drum Rack matches except names, samples, seeds and One-Shot.
- Not checked on a Push: loading the new files, the CRC of 0, One-Shot pads.

## Layout
`src/audio` edits and history, `src/dsp` stretch and pitch, `src/slicer`, `src/wavio`,
`src/autosample`, `src/export`, `src/record`, `src/playback`, `src/library`, `src/app`
(controller), `src/ui` (screen), `src/main.go` (wiring).

## Not verified on a device
The new presets in Live, the Link input on a Push, MIDI out port, audio bus preview, Sample Manager.
