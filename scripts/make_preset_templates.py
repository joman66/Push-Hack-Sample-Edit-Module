#!/usr/bin/env python3
"""Build src/export/tmpl/*.xml from three presets that a real Live 12.4.6 saved.

Usage: make_preset_templates.py Simpler.adv Sampler.adv "Drum Rack.adg"

Each reference is a Simpler with a sample, a Sampler with several zones, and
a Drum Rack with Simpler pads. The script cuts out the parts that change per
export and leaves {{PLACEHOLDERS}} there. Everything else stays byte for byte
what Live wrote, because Live refuses a preset that is missing nodes.
The Sampler reference is a factory preset with its own sound (filter, pitch
envelope, sub oscillator, +12 transpose). The script resets those to the
values of the plain Simpler, so an exported Sampler plays the samples as they are.
"""
import gzip, re, sys, pathlib

out = pathlib.Path(__file__).resolve().parent.parent / "src/export/tmpl"
simp, samp, drum = [gzip.open(p).read().decode() for p in sys.argv[1:4]]

PARTS = re.compile(r"<SampleParts>.*?</SampleParts>|<SampleParts />", re.S)


def cut_parts(s, n=1):
    s, k = PARTS.subn("{{PARTS}}", s)
    assert k == n, (k, n)
    return s


def leaf_values(s):
    """path -> list of (line index, value) for every leaf with a Value."""
    stack, res = [], {}
    lines = s.split("\n")
    for i, l in enumerate(lines):
        t = l.strip()
        m = re.match(r"<([\w.]+)([^>]*?)(/?)>$", t)
        if t.startswith("</"):
            stack.pop()
        elif m:
            name, attrs, selfclose = m.groups()
            if selfclose:
                v = re.search(r'\bValue="([^"]*)"', attrs)
                if v:
                    res.setdefault("/".join(stack + [name]), []).append((i, v.group(1)))
            else:
                stack.append(name)
    return lines, res


def set_line(lines, i, value):
    lines[i] = re.sub(r'Value="[^"]*"', f'Value="{value}"', lines[i], count=1)


# ---- Simpler -------------------------------------------------------------
s_lines, s_vals = leaf_values(cut_parts(simp))
s_text = "\n".join(s_lines)


def sub_path(lines, vals, path, new, which=0):
    i, _ = vals[path][which]
    set_line(lines, i, new)


sub_path(s_lines, s_vals, "Ableton/OriginalSimpler/Pitch/TransposeKey/Manual", "{{TRANSPOSE_KEY}}")
sub_path(s_lines, s_vals, "Ableton/OriginalSimpler/Pitch/TransposeFine/Manual", "{{TRANSPOSE_FINE}}")
sub_path(s_lines, s_vals, "Ableton/OriginalSimpler/Globals/PlaybackMode", "{{PLAYBACK}}")
simpler = "\n".join(s_lines)
(out / "simpler.xml").write_text(simpler)

# ---- Sampler: reset the factory sound to the plain Simpler's values --------
m_lines, m_vals = leaf_values(cut_parts(samp))
_, ref = leaf_values(simp.split("<SampleParts>")[0] + "<SampleParts />" + simp.split("</SampleParts>")[1])
strip = lambda p: p.split("/", 2)[2]
ref_by = {strip(k): v for k, v in ref.items()}
SKIP = ("RoundRobinRandomSeed", "LoopModulators", "InterpolationMode", "UseConstPowCrossfade",
        "IsSimpler", "SubOsc", "LastPresetRef", "UserName", "ViewSettings", "SimplerSlicing")
n = 0
for path, items in m_vals.items():
    p = strip(path)
    if any(k in p for k in SKIP) or p not in ref_by or len(ref_by[p]) != len(items):
        continue
    for (i, mv), (_, sv) in zip(items, ref_by[p]):
        if mv != sv:
            set_line(m_lines, i, sv)
            n += 1
for path, items in m_vals.items():
    if strip(path) == "Player/SubOsc/IsOn/Manual":
        set_line(m_lines, items[0][0], "false")
    if strip(path) == "UserName":
        set_line(m_lines, items[0][0], "")
sampler = "\n".join(m_lines)
# the factory preset reference becomes the blank "new preset" reference
lp = re.compile(r"<LastPresetRef>.*?</LastPresetRef>", re.S)
blank = lp.search(simp).group(0).replace("OriginalSimpler", "MultiSampler")
sampler = lp.sub(lambda _: blank, sampler, count=1)
sampler = sampler.replace("{{PARTS}}", "{{PARTS}}")
sampler = re.sub(r'(<RoundRobinRandomSeed Value=")[^"]*', r"\1-1659344615", sampler)
(out / "sampler.xml").write_text(sampler)
print("sampler values reset:", n)

# ---- Drum Rack: header, one branch, footer --------------------------------
i0 = drum.index('<DrumBranchPreset Id="0">')
i0 = drum.rfind("\n", 0, i0) + 1
j0 = drum.index("</DrumBranchPreset>") + len("</DrumBranchPreset>\n")
i1 = drum.index('<DrumBranchPreset Id="1">')
i1 = drum.rfind("\n", 0, i1) + 1
head, branch, tail = drum[:i0], drum[i0:i1], drum[drum.rindex("</DrumBranchPreset>") + len("</DrumBranchPreset>\n"):]
branch = branch.replace('<DrumBranchPreset Id="0">', '<DrumBranchPreset Id="{{ID}}">', 1)
branch = branch.replace('<Name Value="" />', '<Name Value="{{NAME}}" />', 1)
branch = cut_parts(branch)
branch = re.sub(r'<RoundRobinRandomSeed Value="[^"]*"', '<RoundRobinRandomSeed Value="{{SEED}}"', branch)
branch = re.sub(r'<ReceivingNote Value="\d+"', '<ReceivingNote Value="{{NOTE}}"', branch)
branch = re.sub(r'<ChokeGroup Value="\d+"', '<ChokeGroup Value="{{CHOKE}}"', branch)
branch = re.sub(r'(<IsSimpler Value="true" />\s*<PlaybackMode Value=")\d+', r"\1{{PLAYBACK}}", branch)
b_lines, b_vals = leaf_values(branch)
for path, items in b_vals.items():
    if path.endswith("OriginalSimpler/Pitch/TransposeKey/Manual"):
        set_line(b_lines, items[0][0], "{{TRANSPOSE_KEY}}")
    if path.endswith("OriginalSimpler/Pitch/TransposeFine/Manual"):
        set_line(b_lines, items[0][0], "{{TRANSPOSE_FINE}}")
branch = "\n".join(b_lines)
(out / "drumrack.xml").write_text(head + "{{BRANCHES}}" + tail)
(out / "drumbranch.xml").write_text(branch)
for k in ("{{ID}}", "{{NAME}}", "{{PARTS}}", "{{SEED}}", "{{NOTE}}", "{{CHOKE}}", "{{PLAYBACK}}", "{{TRANSPOSE_KEY}}", "{{TRANSPOSE_FINE}}"):
    assert branch.count(k) == 1, (k, branch.count(k))
print("ok")
