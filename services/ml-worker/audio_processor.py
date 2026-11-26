import json
import os
import subprocess
import wave
from typing import Tuple


def probe_duration_seconds(file_path: str) -> int:
    """Measures media duration using ffprobe or falls back to WAV header inspection."""
    try:
        out = subprocess.check_output(
            [
                "ffprobe",
                "-v",
                "error",
                "-show_entries",
                "format=duration",
                "-of",
                "json",
                file_path,
            ],
            stderr=subprocess.DEVNULL,
            timeout=30,
        )
        data = json.loads(out.decode("utf-8"))
        dur = float(data.get("format", {}).get("duration", 0))
        if dur > 0:
            return max(1, int(round(dur)))
    except Exception:
        pass

    if file_path.lower().endswith(".wav") and os.path.exists(file_path):
        try:
            with wave.open(file_path, "rb") as w:
                frames = w.getnframes()
                rate = w.getframerate()
                if rate > 0:
                    return max(1, int(round(frames / float(rate))))
        except Exception:
            pass
    return 180


def normalize_to_wav_16k(input_path: str, output_wav_path: str) -> Tuple[str, int]:
    """
    Converts arbitrary audio (mp3/m4a/wav/aac/ogg/webm) into 16kHz mono PCM WAV
    with loudness normalization suitable for Whisper STT.
    """
    cmd = [
        "ffmpeg",
        "-y",
        "-i",
        input_path,
        "-vn",
        "-ac",
        "1",
        "-ar",
        "16000",
        "-af",
        "loudnorm",
        "-c:a",
        "pcm_s16le",
        output_wav_path,
    ]
    subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=300)
    duration_sec = probe_duration_seconds(output_wav_path)
    return output_wav_path, duration_sec
