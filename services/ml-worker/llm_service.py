import json
import os
import uuid
from typing import Dict


SYSTEM_PROMPT = (
    "Ты медицинский ассистент врача акушера-гинеколога (курс «Беременность без тревоги»). "
    "Проанализируй обезличенный диалог консультации врача и беременной пациентки. "
    "Составь структурированное клиническое саммари строго в JSON с полями: "
    "complaints, anamnesis, recommendations, diagnostics_plan, follow_up. "
    "Не выдумывай дозировки лекарств, которых не было в диалоге; пиши бережно, понятно и точно."
)


def _parse_llm_json(raw_text: str) -> Dict[str, str]:
    text = raw_text.strip()
    if "```" in text:
        parts = text.split("```")
        for p in parts:
            cleaned = p.lstrip("json").strip()
            if cleaned.startswith("{"):
                text = cleaned
                break
    data = json.loads(text)
    return {
        "complaints": str(data.get("complaints", "")).strip(),
        "anamnesis": str(data.get("anamnesis", "")).strip(),
        "recommendations": str(data.get("recommendations", "")).strip(),
        "diagnostics_plan": str(data.get("diagnostics_plan", "")).strip(),
        "follow_up": str(data.get("follow_up", "")).strip(),
    }


def _mock_summary(masked_transcript: str, patient_question: str, preg_status: str) -> Dict[str, str]:
    complaints = patient_question.strip() or "Умеренная утомляемость во второй половине дня, тревожность перед плановым обследованием."
    term = preg_status.strip() or "Беременность (плановое наблюдение)"
    return {
        "llm_model_name": "mock-ru-obgyn-v1",
        "complaints": complaints,
        "anamnesis": f"Статус: {term}. В ходе консультации разобраны динамика самочувствия, режим сна и текущие показатели крови.",
        "recommendations": (
            "1. Соблюдать щадящий режим труда и отдыха: сон 8 часов, дневные прогулки на свежем воздухе 30–40 минут.\n"
            "2. Полноценное питание с достаточным содержанием белка и продуктов, богатых железом.\n"
            "3. Техники снижения тревожности и дыхательные упражнения из Модуля 2 курса.\n"
            "4. Приём препаратов железа строго по схеме, согласованной врачом на очном/телемедицинском приёме."
        ),
        "diagnostics_plan": "Контроль ферритина, сывороточного железа и общего анализа крови через 4 недели.",
        "follow_up": "Повторная плановая консультация через 1 месяц (или внепланово при ухудшении самочувствия).",
    }


def _call_yandexgpt(masked_transcript: str, context_note: str) -> Dict[str, str]:
    import requests
    api_key = os.getenv("YANDEX_GPT_API_KEY") or os.getenv("YANDEX_API_KEY", "")
    folder_id = os.getenv("YANDEX_GPT_FOLDER_ID") or os.getenv("YANDEX_FOLDER_ID", "")
    if not api_key or not folder_id:
        raise RuntimeError("YANDEX_GPT_API_KEY and YANDEX_GPT_FOLDER_ID are required when LLM_PROVIDER=yandexgpt")

    model_uri = f"gpt://{folder_id}/yandexgpt/latest"
    resp = requests.post(
        "https://llm.api.cloud.yandex.net/foundationModels/v1/completion",
        headers={"Authorization": f"Api-Key {api_key}", "x-folder-id": folder_id},
        json={
            "modelUri": model_uri,
            "completionOptions": {"stream": False, "temperature": 0.2, "maxTokens": 1800},
            "messages": [
                {"role": "system", "text": SYSTEM_PROMPT},
                {"role": "user", "text": f"Контекст: {context_note}\n\nСтенограмма:\n{masked_transcript}"},
            ],
        },
        timeout=60,
    )
    resp.raise_for_status()
    text = resp.json()["result"]["alternatives"][0]["message"]["text"]
    parsed = _parse_llm_json(text)
    parsed["llm_model_name"] = "yandexgpt/latest"
    return parsed


def _call_gigachat(masked_transcript: str, context_note: str) -> Dict[str, str]:
    import requests
    auth_key = os.getenv("GIGACHAT_AUTH_KEY", "")
    scope = os.getenv("GIGACHAT_SCOPE", "GIGACHAT_API_PERS")
    if not auth_key:
        raise RuntimeError("GIGACHAT_AUTH_KEY is required when LLM_PROVIDER=gigachat")

    oauth_resp = requests.post(
        "https://ngw.devices.sberbank.ru:9443/api/v2/oauth",
        headers={
            "Content-Type": "application/x-www-form-urlencoded",
            "Accept": "application/json",
            "RqUID": str(uuid.uuid4()),
            "Authorization": f"Basic {auth_key}",
        },
        data={"scope": scope},
        timeout=30,
    )
    oauth_resp.raise_for_status()
    access_token = oauth_resp.json()["access_token"]

    chat_resp = requests.post(
        "https://gigachat.devices.sberbank.ru/api/v1/chat/completions",
        headers={"Authorization": f"Bearer {access_token}", "Content-Type": "application/json"},
        json={
            "model": "GigaChat-Pro",
            "temperature": 0.2,
            "messages": [
                {"role": "system", "content": SYSTEM_PROMPT},
                {"role": "user", "content": f"Контекст: {context_note}\n\nСтенограмма:\n{masked_transcript}"},
            ],
        },
        timeout=60,
    )
    chat_resp.raise_for_status()
    text = chat_resp.json()["choices"][0]["message"]["content"]
    parsed = _parse_llm_json(text)
    parsed["llm_model_name"] = "GigaChat-Pro"
    return parsed


def generate_summary(masked_transcript: str, patient_question: str = "", preg_status: str = "") -> Dict[str, str]:
    provider = os.getenv("LLM_PROVIDER", "mock").lower()
    context_note = f"Срок/статус: {preg_status or 'не указан'}; запрос пациентки: {patient_question or 'плановая консультация'}"
    if provider == "yandexgpt":
        return _call_yandexgpt(masked_transcript, context_note)
    if provider == "gigachat":
        return _call_gigachat(masked_transcript, context_note)
    return _mock_summary(masked_transcript, patient_question, preg_status)
