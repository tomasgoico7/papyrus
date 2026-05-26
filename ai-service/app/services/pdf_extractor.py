import logging
from io import BytesIO

from pypdf import PdfReader

logger = logging.getLogger(__name__)


class UnreadableCVError(Exception):
    """Raised when a CV cannot be turned into analyzable text."""


def extract_text(data: bytes, max_chars: int) -> str:
    """Extract plain text from a PDF.

    Scanned PDFs with no embedded text layer, encrypted files and corrupt
    uploads all surface as `UnreadableCVError` so the caller can return a single,
    actionable error rather than leaking a parser exception.
    """
    try:
        reader = PdfReader(BytesIO(data))
        pages = [page.extract_text() or "" for page in reader.pages]
    except Exception as exc:  # noqa: BLE001 — any parser failure means "unreadable"
        logger.warning("PDF parsing failed: %s", exc)
        raise UnreadableCVError("The CV is not a readable PDF.") from exc

    text = "\n".join(part for part in pages if part).strip()
    if not text:
        raise UnreadableCVError(
            "No text could be extracted from the CV. It may be a scanned image."
        )

    return text[:max_chars]
