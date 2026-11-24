import os
from typing import Dict, List


def _mock_segments(duration_sec: int, patient_question: str = "") -> List[Dict]:
    dur = max(12, int(duration_sec or 180))
    step = max(3.0, min(8.0, dur / 6.0))
    q_snippet = patient_question.strip() or "лёгкая утомляемость во второй половине дня и тревожность перед скринингом"
    utterances = [
        ("Врач", "Здравствуйте! Расскажите, как вы себя чувствуете с момента прошлой недели и что сейчас беспокоит больше всего?"),
        ("Пациент", f"Здравствуйте, доктор! Меня беспокоит: {q_snippet}. Хочу понять, норма ли это на моём сроке."),
        ("Врач", "Понимаю вашу тревогу, давайте спокойно разберём результаты анализов и самочувствие. По общему анализу крови ферритин 24 мкг/л — это умеренный латентный дефицит железа."),
        ("Врач", "Рекомендую скорректировать питание, добавить умеренные прогулки по 30 минут в день, соблюдать режим сна и согласованный приём железа под контролем крови через 4 недели."),
        ("Пациент", "Спасибо большое, стало гораздо спокойнее! А когда нужно сдать повторные анализы?"),
        ("Врач", "Через 28 дней повторим ферритин и клинический анализ крови, а через месяц проведём контрольную консультацию. Все рекомендации я оформлю и отправлю вам в кабинет."),
    ]
    segments = []
    cur = 0.0
    for idx, (speaker, text) in enumerate(utterances):
        end = min(float(dur), round(cur + step, 2))
        if end <= cur:
            end = round(cur + 2.0, 2)
        segments.append(
            {
                "segment_index": idx,
                "speaker_label": speaker,
                "start_time_sec": round(cur, 2),
                "end_time_sec": end,
                "text": text,
                "confidence": 0.96,
            }
        )
        cur = end
    return segments


def transcribe_audio(wav_path: str, duration_sec: int, patient_question: str = "") -> List[Dict]:
    """
    Runs STT using Whisper (`faster_whisper` if STT_PROVIDER=faster_whisper) or realistic mock mode.
    """
    provider = os.getenv("STT_PROVIDER", "mock").lower()
    if provider == "faster_whisper":
        try:
            from faster_whisper import WhisperModel  # type: ignore

            model_size = os.getenv("WHISPER_MODEL", "base")
            device = os.getenv("WHISPER_DEVICE", "cpu")
            compute_type = os.getenv("WHISPER_COMPUTE_TYPE", "int8")
            model = WhisperModel(model_size, device=device, compute_type=compute_type)
            raw_segments, _ = model.transcribe(wav_path, language="ru", vad_filter=True)
            out: List[Dict] = []
            for idx, seg in enumerate(raw_segments):
                out.append(
                    {
                        "segment_index": idx,
                        "speaker_label": "Речь",
                        "start_time_sec": round(float(seg.start), 2),
                        "end_time_sec": round(float(seg.end), 2),
                        "text": seg.text.strip(),
                        "confidence": 0.92,
                    }
                )
            if out:
                return out
        except ImportError as exc:
            raise RuntimeError("STT_PROVIDER=faster_whisper configured, but faster_whisper package is not installed") from exc

    return _mock_segments(duration_sec, patient_question=patient_question)
