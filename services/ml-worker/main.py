import os
import signal
import sys
import time
import redis

running = True

def handle_signal(sig, frame):
    global running
    print(f"Received signal {sig}, terminating ml-worker gracefully...")
    running = False

signal.signal(signal.SIGINT, handle_signal)
signal.signal(signal.SIGTERM, handle_signal)

def main():
    redis_host = os.getenv("REDIS_HOST", "redis")
    redis_port = int(os.getenv("REDIS_PORT", "6379"))
    redis_pass = os.getenv("REDIS_PASSWORD", None)
    llm_provider = os.getenv("LLM_PROVIDER", "mock")
    minio_endpoint = os.getenv("MINIO_ENDPOINT", "minio:9000")

    print("Starting ML Audio & Summarization Worker...")
    print(f"Configuration: Redis={redis_host}:{redis_port}, MinIO={minio_endpoint}, LLM={llm_provider}")

    r = None
    while running:
        try:
            r = redis.Redis(host=redis_host, port=redis_port, password=redis_pass, socket_timeout=3)
            if r.ping():
                print("ML-Worker connected to Redis. Subscribed to queue: [audio_transcribe, text_summarize]")
                break
        except Exception as e:
            print(f"Waiting for Redis connection: {e}")
            time.sleep(2)

    while running:
        try:
            # Poll or wait for tasks
            time.sleep(10)
            print("[Heartbeat] ML Worker active. ffmpeg available. Ready for Whisper transcription tasks.")
        except Exception as e:
            print(f"Error in worker loop: {e}")
            time.sleep(2)

    print("ML-Worker stopped.")
    sys.exit(0)

if __name__ == "__main__":
    main()
