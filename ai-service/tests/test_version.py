from fastapi.testclient import TestClient

from app.main import app
from app.services.prompt import PROMPT_VERSION, _fingerprint


def test_version_reports_what_the_cache_key_depends_on() -> None:
    with TestClient(app) as client:
        response = client.get("/version")

    assert response.status_code == 200
    body = response.json()
    assert body["promptVersion"] == PROMPT_VERSION
    assert body["model"]


def test_the_fingerprint_is_stable_across_calls() -> None:
    # A version that moved on its own would invalidate every cached analysis on
    # each restart.
    assert _fingerprint("a", "b") == _fingerprint("a", "b")


def test_the_fingerprint_follows_the_prompt() -> None:
    assert _fingerprint("system", "human") != _fingerprint("system!", "human")


def test_the_fingerprint_cannot_be_confused_by_a_different_split() -> None:
    # Length prefixing, rather than a separator, is what makes this hold.
    assert _fingerprint("ab", "c") != _fingerprint("a", "bc")


def test_the_prompt_version_is_short_enough_for_a_cache_key() -> None:
    assert len(PROMPT_VERSION) == 12
    assert PROMPT_VERSION.isalnum()
