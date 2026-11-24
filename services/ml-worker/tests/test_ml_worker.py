import math
import os
import shutil
import struct
import tempfile
import unittest
import wave

from pii_masker import mask_pii
from audio_processor import normalize_to_wav_16k, probe_duration_seconds
from stt_engine import transcribe_audio
from llm_service import generate_summary


class TestMLWorker(unittest.TestCase):
    def test_pii_masking(self):
        raw = (
            "Пациентка Кузнецова Анна, телефон +7 (999) 123-45-67, запасной 89165554433, "
            "почта anna.test@example.ru, СНИЛС 123-456-789 00, полис 1234567890123456, паспорт 45 11 123456."
        )
        masked = mask_pii(raw, patient_name="Кузнецова Анна")
        self.assertNotIn("999", masked)
        self.assertNotIn("89165554433", masked)
        self.assertNotIn("anna.test@example.ru", masked)
        self.assertNotIn("123-456-789 00", masked)
        self.assertNotIn("1234567890123456", masked)
        self.assertNotIn("45 11 123456", masked)
        self.assertNotIn("Кузнецова", masked)
        self.assertIn("[EMAIL]", masked)
        self.assertIn("[ТЕЛЕФОН]", masked)
        self.assertIn("[СНИЛС]", masked)
        self.assertIn("[ПАЦИЕНТ]", masked)

    def test_mock_stt_and_summary(self):
        os.environ["STT_PROVIDER"] = "mock"
        os.environ["LLM_PROVIDER"] = "mock"
        segments = transcribe_audio("dummy.wav", 120, patient_question="Беспокоит низкий ферритин")
        self.assertGreaterEqual(len(segments), 5)
        self.assertEqual(segments[0]["segment_index"], 0)
        self.assertLess(segments[0]["start_time_sec"], segments[0]["end_time_sec"])

        summary = generate_summary("Врач: всё хорошо", patient_question="Низкий ферритин", preg_status="22 недели")
        self.assertEqual(summary["llm_model_name"], "mock-ru-obgyn-v1")
        self.assertIn("ферритин", summary["complaints"].lower())
        self.assertTrue(len(summary["recommendations"]) > 30)

    def test_audio_processing_with_ffmpeg_or_wav(self):
        with tempfile.TemporaryDirectory() as td:
            src_wav = os.path.join(td, "tone.wav")
            out_wav = os.path.join(td, "norm.wav")
            sample_rate = 8000
            n_frames = sample_rate * 2  # 2 seconds
            with wave.open(src_wav, "wb") as w:
                w.setnchannels(1)
                w.setsampwidth(2)
                w.setframerate(sample_rate)
                frames = bytearray()
                for i in range(n_frames):
                    val = int(4000 * math.sin(2 * math.pi * 440.0 * i / sample_rate))
                    frames.extend(struct.pack("<h", val))
                w.writeframes(bytes(frames))

            if shutil.which("ffmpeg"):
                norm_path, dur = normalize_to_wav_16k(src_wav, out_wav)
                self.assertTrue(os.path.exists(norm_path))
                self.assertGreaterEqual(dur, 1)
            else:
                dur = probe_duration_seconds(src_wav)
                self.assertEqual(dur, 2)


if __name__ == "__main__":
    unittest.main()
