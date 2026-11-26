import os
from typing import Dict, List, Optional, Tuple
import psycopg


def get_dsn() -> str:
    host = os.getenv("POSTGRES_HOST", "postgres")
    port = os.getenv("POSTGRES_PORT", "5432")
    db = os.getenv("POSTGRES_DB", "zdorovie_db")
    user = os.getenv("POSTGRES_USER", "zdorovie_user")
    pw = os.getenv("POSTGRES_PASSWORD", "zdorovie_pass")
    return f"postgresql://{user}:{pw}@{host}:{port}/{db}"


def connect():
    return psycopg.connect(get_dsn(), autocommit=False)


def get_slot_and_patient_context(record_id: int) -> Tuple[str, str, str]:
    """Returns (patient_full_name, pregnancy_status, client_question) for a record."""
    with connect() as conn, conn.cursor() as cur:
        cur.execute(
            """
            SELECT COALESCE(u.full_name, ''), COALESCE(u.pregnancy_status, ''), COALESCE(cs.client_question, '')
            FROM consultation_records cr
            LEFT JOIN users u ON u.id = cr.client_id
            LEFT JOIN consultation_slots cs ON cs.id = cr.slot_id
            WHERE cr.id = %s
            """,
            (record_id,),
        )
        row = cur.fetchone()
        if not row:
            return "", "", ""
        return row[0], row[1], row[2]


def update_record_status(
    record_id: int,
    status: str,
    duration_seconds: Optional[int] = None,
    error_message: str = "",
) -> None:
    with connect() as conn, conn.cursor() as cur:
        cur.execute(
            """
            UPDATE consultation_records
            SET status = %s,
                duration_seconds = COALESCE(%s, duration_seconds),
                error_message = NULLIF(%s, '')
            WHERE id = %s
            """,
            (status, duration_seconds, error_message, record_id),
        )
        conn.commit()


def replace_transcript_segments(record_id: int, segments: List[Dict]) -> None:
    with connect() as conn, conn.cursor() as cur:
        cur.execute("DELETE FROM consultation_transcripts WHERE record_id = %s", (record_id,))
        for seg in segments:
            cur.execute(
                """
                INSERT INTO consultation_transcripts
                    (record_id, segment_index, speaker_label, start_time_sec, end_time_sec, text, confidence)
                VALUES (%s, %s, %s, %s, %s, %s, %s)
                """,
                (
                    record_id,
                    int(seg["segment_index"]),
                    str(seg["speaker_label"]),
                    float(seg["start_time_sec"]),
                    float(seg["end_time_sec"]),
                    str(seg["text"]),
                    float(seg.get("confidence", 0.95)),
                ),
            )
        conn.commit()


def fetch_transcript_text(record_id: int) -> str:
    with connect() as conn, conn.cursor() as cur:
        cur.execute(
            """
            SELECT speaker_label, start_time_sec, text
            FROM consultation_transcripts
            WHERE record_id = %s
            ORDER BY segment_index ASC
            """,
            (record_id,),
        )
        lines = []
        for speaker, start_sec, text in cur.fetchall():
            mins = int(float(start_sec) // 60)
            secs = int(float(start_sec) % 60)
            lines.append(f"[{mins:02d}:{secs:02d}] {speaker}: {text}")
        return "\n".join(lines)


def save_summary_draft(
    record_id: int,
    slot_id: int,
    doctor_id: int,
    client_id: int,
    summary: Dict[str, str],
) -> int:
    with connect() as conn, conn.cursor() as cur:
        cur.execute("SELECT id FROM consultation_summaries WHERE record_id = %s AND status = 'draft' LIMIT 1", (record_id,))
        existing = cur.fetchone()
        if existing:
            sum_id = existing[0]
            cur.execute(
                """
                UPDATE consultation_summaries
                SET llm_model_name = %s, complaints = %s, anamnesis = %s, recommendations = %s,
                    diagnostics_plan = %s, follow_up = %s, updated_at = NOW()
                WHERE id = %s
                """,
                (
                    summary.get("llm_model_name", "mock"),
                    summary.get("complaints", ""),
                    summary.get("anamnesis", ""),
                    summary.get("recommendations", ""),
                    summary.get("diagnostics_plan", ""),
                    summary.get("follow_up", ""),
                    sum_id,
                ),
            )
            conn.commit()
            return int(sum_id)

        cur.execute(
            """
            INSERT INTO consultation_summaries
                (record_id, slot_id, doctor_id, client_id, llm_model_name, complaints, anamnesis, recommendations, diagnostics_plan, follow_up, status)
            VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, 'draft')
            RETURNING id
            """,
            (
                record_id,
                slot_id,
                doctor_id,
                client_id,
                summary.get("llm_model_name", "mock"),
                summary.get("complaints", ""),
                summary.get("anamnesis", ""),
                summary.get("recommendations", ""),
                summary.get("diagnostics_plan", ""),
                summary.get("follow_up", ""),
            ),
        )
        new_id = cur.fetchone()[0]
        conn.commit()
        return int(new_id)
