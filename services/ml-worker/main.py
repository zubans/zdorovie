import json
import os
import signal
import sys
import tempfile
import time
from typing import Optional
from minio import Minio
import redis

from audio_processor import normalize_to_wav_16k
import db_client
from llm_service import generate_summary
from pii_masker import mask_pii
from stt_engine import transcribe_audio

QUEUE_TRANSCRIBE = "zdorovie:queue:audio_transcribe"
QUEUE_SUMMARIZE = "zdorovie:queue:text_summarize"
QUEUE_FAILED = "zdorovie:queue:ml_failed"
STATS_HASH = "zdorovie:stats:ml"

running = True


def handle_signal(sig, frame):
    global running
    print(f"Received signal {sig}, terminating ml-worker gracefully...")
    running = False


signal.signal(signal.SIGINT, handle_signal)
signal.signal(signal.SIGTERM, handle_signal)


def get_minio_client() -> Minio:
    endpoint = os.getenv("MINIO_ENDPOINT", "minio:9000")
    access_key = os.getenv("MINIO_ROOT_USER", "minioadmin")
    secret_key = os.getenv("MINIO_ROOT_PASSWORD", "minioadmin123")
    secure = os.getenv("MINIO_USE_SSL", "false").lower() == "true"
    return Minio(endpoint, access_key=access_key, secret_key=secret_key, secure=secure)


def process_transcribe_job(payload: dict, s3: Minio, r: redis.Redis) -> None:
    record_id = int(payload["record_id"])
    slot_id = int(payload["slot_id"])
    doctor_id = int(payload["doctor_id"])
    client_id = int(payload["client_id"])
    s3_key = payload["s3_audio_key"]
    audio_fmt = payload.get("audio_format", "wav")
    bucket = os.getenv("MINIO_BUCKET_CONSULTATIONS", "zdorovie-consultations")

    print(f"[ML-Worker] Starting audio_transcribe record={record_id} slot={slot_id}")
    db_client.update_record_status(record_id, "transcribing")

    patient_name, _, client_question = db_client.get_slot_and_patient_context(record_id)

    with tempfile.TemporaryDirectory(prefix="zdorovie_ml_") as tmpdir:
        src_path = os.path.join(tmpdir, f"input.{audio_fmt}")
        wav_path = os.path.join(tmpdir, "normalized_16k.wav")
        s3.fget_object(bucket, s3_key, src_path)

        normalized_wav, duration_sec = normalize_to_wav_16k(src_path, wav_path)
        segments = transcribe_audio(normalized_wav, duration_sec, patient_question=client_question)

        # Mask PII in stored dialog segments only? Store accurate clinical text in DB (doctor owns it),
        # and mask before external LLM calls. Note: also ensure segments written reliably.
        db_client.replace_transcript_segments(record_id, segments)
        db_client.update_record_status(record_id, "summarizing", duration_seconds=duration_sec)

    # Enqueue next stage: text_summarize
    r.lpush(
        QUEUE_SUMMARIZE,
        json.dumps(
            {
                "record_id": record_id,
                "slot_id": slot_id,
                "doctor_id": doctor_id,
                "client_id": client_id,
            }
        ),
    )


def process_summarize_job(payload: dict) -> None:
    record_id = int(payload["record_id"])
    slot_id = int(payload["slot_id"])
    doctor_id = int(payload["doctor_id"])
    client_id = int(payload["client_id"])

    print(f"[ML-Worker] Starting text_summarize record={record_id} slot={slot_id}")
    patient_name, preg_status, client_question = db_client.get_slot_and_patient_context(record_id)
    raw_transcript = db_client.fetch_transcript_text(record_id)
    masked_transcript = mask_pii(raw_transcript, patient_name=patient_name)
    masked_question = mask_pii(client_question, patient_name=patient_name)

    summary = generate_summary(masked_transcript, patient_question=masked_question, preg_status=preg_status)
    sum_id = db_client.save_summary_draft(record_id, slot_id, doctor_id, client_id, summary)
    db_client.update_record_status(record_id, "completed")
    print(f"[ML-Worker] Draft summary id={sum_id} ready for doctor HITL review (record={record_id})")


def handle_queue_message(queue_name: str, raw_body: str, s3: Optional[Minio], r: redis.Redis) -> None:
    payload = json.loads(raw_body)
    try:
        if queue_name == QUEUE_TRANSCRIBE:
            process_transcribe_job(payload, s3 or get_minio_client(), r)
        elif queue_name == QUEUE_SUMMARIZE:
            process_summarize_job(payload)
        r.hincrby(STATS_HASH, "processed", 1)
    except Exception as exc:
        r.hincrby(STATS_HASH, "failed", 1)
        record_id = payload.get("record_id")
        if record_id:
            try:
                db_client.update_record_status(int(record_id), "failed", error_message=str(exc))
            except Exception:
                pass
        r.lpush(QUEUE_FAILED, json.dumps({"queue": queue_name, "payload": payload, "error": str(exc)}))
        print(f"[ML-Worker] ERROR processing {queue_name} record={record_id}: {exc}")


def main():
    redis_host = os.getenv("REDIS_HOST", "redis")
    redis_port = int(os.getenv("REDIS_PORT", "6379"))
    redis_pass = os.getenv("REDIS_PASSWORD", None) or None
    llm_provider = os.getenv("LLM_PROVIDER", "mock")
    stt_provider = os.getenv("STT_PROVIDER", "mock")
    minio_endpoint = os.getenv("MINIO_ENDPOINT", "minio:9000")

    print("Starting ML Audio & Summarization Worker...")
    print(f"Configuration: Redis={redis_host}:{redis_port}, MinIO={minio_endpoint}, STT={stt_provider}, LLM={llm_provider}")

    r = None
    while running:
        try:
            r = redis.Redis(host=redis_host, port=redis_port, password=redis_pass, decode_responses=True, socket_timeout=10)
            if r.ping():
                print(f"ML-Worker connected to Redis. Watching queues: [{QUEUE_TRANSCRIBE}, {QUEUE_SUMMARIZE}]")
                break
        except Exception as e:
            print(f"Waiting for Redis connection: {e}")
            time.sleep(2)

    s3 = get_minio_client()

    while running:
        try:
            item = r.brpop([QUEUE_TRANSCRIBE, QUEUE_SUMMARIZE], timeout=3)
            if not item:
                continue
            qname, body = item
            handle_queue_message(qname, body, s3, r)
        except Exception as e:
            print(f"Error in worker loop: {e}")
            time.sleep(2)

    print("ML-Worker stopped.")
    sys.exit(0)


if __name__ == "__main__":
    main()
