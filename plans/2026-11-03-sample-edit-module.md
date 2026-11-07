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
- Recording uses `arecord` (S32_LE) so there is no cgo. It opens only while armed.
- Time stretch is WSOLA. Pitch shift is stretch plus resample.

## Layout
`src/audio` edits and history, `src/dsp` stretch and pitch, `src/slicer`, `src/wavio`,
`src/autosample`, `src/export`, `src/record`, `src/playback`, `src/library`, `src/app`
(controller), `src/ui` (screen), `src/main.go` (wiring).

## Not verified on a device
Presets in Live, `arecord` on Push, input device names, MIDI out port, audio bus preview.
