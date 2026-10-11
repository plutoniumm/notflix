#!/usr/bin/env python3
import sys, json
from faster_whisper import WhisperModel

audio_path = sys.argv[1]
# "small", not "base". Measured against City Hunter ep05's release sub (210
# deduped dialogue cues), scored on cue-onset recall within 2s — a metric that,
# unlike temporal overlap, can't be gamed by an overlong cue: base 74.3% recall /
# 17.2% spurious, small 75.7% / 12.0%. Same recall, a third less junk, and 258
# cues instead of 320 so the ollama translate pass has 20% less to do. Costs
# 217s for a 25-min episode (~7x realtime) under `nice`. ASR is the ceiling on
# the whole subtitle pipeline — translation quality is already fine.
model_name = sys.argv[2] if len(sys.argv) > 2 else "small"

model = WhisperModel(model_name, device="cpu", compute_type="int8")
# condition_on_previous_text stays at its default (True). Turning it off was
# tried and MEASURED WORSE against City Hunter ep05's release sub: onset recall
# 75.7% -> 72.4% at the same model, for no drop in spurious cues. The long cues
# that motivated it are a TIMESTAMP artifact, not a decoder one, and whisperVTT's
# clamp in whisper.go fixes those outright (17 cues >15s -> 0).
# VAD parameters are left alone too — threshold/max_speech_duration_s tuning
# measured worse (91.0% vs 91.4% overlap, run-on count unchanged).
segments, info = model.transcribe(
    audio_path, beam_size=5, vad_filter=True, task="transcribe",
)

print(json.dumps({"lang": info.language}), flush=True)

for seg in segments:
    print(json.dumps({"start": seg.start, "end": seg.end, "text": seg.text.strip()}), flush=True)
