import re
from typing import Optional


_PHONE_RE = re.compile(r"(?:(?:\+?7|8)[\s\-()]*(?:\d[\s\-()]*){10})")
_EMAIL_RE = re.compile(r"[a-zA-Z0-9_.+-]+@[a-zA-Z0-9-]+\.[a-zA-Z0-9-.]+")
_SNILS_RE = re.compile(r"\b\d{3}[-\s]\d{3}[-\s]\d{3}[-\s]\d{2}\b")
_OMS_16_RE = re.compile(r"\b(?:\d{4}[\s-]?){4}\b")
_PASSPORT_RE = re.compile(r"\b\d{2}\s?\d{2}\s+\d{6}\b")


def mask_pii(text: str, patient_name: Optional[str] = None) -> str:
    """
    Anonymizes patient PII (152-FZ compliance) before dispatching text to external LLMs.
    Replaces phones, emails, SNILS, 16-digit OMS/card numbers, passport IDs, and optional patient name parts.
    """
    if not text:
        return ""
    masked = text
    masked = _EMAIL_RE.sub("[EMAIL]", masked)
    masked = _SNILS_RE.sub("[СНИЛС]", masked)
    masked = _PASSPORT_RE.sub("[ПАСПОРТ]", masked)
    masked = _OMS_16_RE.sub("[ПОЛИС_ОМС]", masked)
    masked = _PHONE_RE.sub("[ТЕЛЕФОН]", masked)

    if patient_name:
        for part in patient_name.strip().split():
            if len(part) >= 3:
                masked = re.sub(re.escape(part), "[ПАЦИЕНТ]", masked, flags=re.IGNORECASE)
    return masked
