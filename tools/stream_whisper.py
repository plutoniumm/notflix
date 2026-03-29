#!/usr/bin/env python3
import sys, json
from faster_whisper import WhisperModel

audio_path = sys.argv[1]
model_name = sys.argv[2] if len(sys.argv) > 2 else "base"

model = WhisperModel(model_name, device="cpu", compute_type="int8")
segments, info = model.transcribe(audio_path, beam_size=5, vad_filter=True, task="transcribe")

print(json.dumps({"lang": info.language}), flush=True)

for seg in segments:
    print(json.dumps({"start": seg.start, "end": seg.end, "text": seg.text.strip()}), flush=True)
