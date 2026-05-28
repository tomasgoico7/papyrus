from io import BytesIO

import pytest
from reportlab.pdfgen import canvas

from app.services.pdf_extractor import UnreadableCVError, extract_text


def _pdf_with_text(text: str) -> bytes:
    buffer = BytesIO()
    pdf = canvas.Canvas(buffer)
    pdf.drawString(72, 720, text)
    pdf.showPage()
    pdf.save()
    return buffer.getvalue()


def test_extracts_text_from_pdf():
    data = _pdf_with_text("Senior Go Engineer with Postgres")
    assert "Senior Go Engineer" in extract_text(data, max_chars=10_000)


def test_truncates_to_max_chars():
    data = _pdf_with_text("abcdefghijklmnopqrstuvwxyz")
    assert len(extract_text(data, max_chars=5)) == 5


def test_rejects_non_pdf_bytes():
    with pytest.raises(UnreadableCVError):
        extract_text(b"this is plainly not a pdf", max_chars=1_000)
