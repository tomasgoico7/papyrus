"""A stand-in for the AI service, for load tests.

It answers the same shapes as the real service without calling a model, so a run
costs nothing and repeats exactly. STUB_LATENCY_MS simulates the provider when
what is being measured is queueing rather than raw throughput.

Standard library only: it runs on the plain python image, with no build.
"""

import json
import os
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LATENCY_SECONDS = float(os.environ.get("STUB_LATENCY_MS", "0")) / 1000
PORT = int(os.environ.get("PORT", "8000"))

ANALYSIS = {
    "score": 74,
    "verdict": "moderate",
    "summary": {"en": "Decent fit.", "es": "Encaje razonable."},
    "matchedSkills": {"en": ["Go", "PostgreSQL"], "es": ["Go", "PostgreSQL"]},
    "missingSkills": {"en": ["Kubernetes"], "es": ["Kubernetes"]},
    "suggestions": [
        {
            "title": {"en": "Quantify impact", "es": "Cuantificá el impacto"},
            "detail": {"en": "Add numbers.", "es": "Agregá números."},
            "priority": "high",
        }
    ],
}

QUESTIONS = {
    "questions": [{"topic": "Kubernetes", "question": "Do you have production experience?"}],
    "cvText": "stubbed cv text",
}

TAILORED = {
    "fullName": "Jane Doe",
    "contact": "jane@example.com",
    "headline": "Backend Engineer",
    "summary": "Stubbed summary.",
    "experience": [],
    "skills": ["Backend: Go, Python"],
    "education": [],
    "additional": [],
}

ROUTES = {
    "/analyze": ANALYSIS,
    "/tailor/questions": QUESTIONS,
    "/tailor/generate": TAILORED,
}


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self) -> None:  # noqa: N802 — the base class defines the name
        if self.path == "/health":
            self._respond(200, {"status": "ok"})
        else:
            self._respond(404, {"error": {"code": "not_found", "message": "Unknown path."}})

    def do_POST(self) -> None:  # noqa: N802 — the base class defines the name
        # The body has to be drained or the client sees a reset connection.
        length = int(self.headers.get("Content-Length", "0"))
        while length > 0:
            length -= len(self.rfile.read(min(length, 65536)))

        payload = ROUTES.get(self.path)
        if payload is None:
            self._respond(404, {"error": {"code": "not_found", "message": "Unknown path."}})
            return

        if LATENCY_SECONDS:
            time.sleep(LATENCY_SECONDS)
        self._respond(200, payload)

    def log_message(self, *_args: object) -> None:
        """Silence the per-request line; it would dominate a load-test log."""

    def _respond(self, status: int, payload: dict[str, object]) -> None:
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    ThreadingHTTPServer(("", PORT), Handler).serve_forever()
