"""Correlation identifier shared with the gateway.

The rules here mirror `gateway/internal/requestid`: same header, same width, same
character set. They have to agree, or the two services log ids that cannot be
joined.
"""

import secrets

HEADER = "X-Request-ID"

# An identifier reaches log lines, so an unbounded caller-supplied value is a
# log-bloat vector.
_MAX_LENGTH = 64
_ALLOWED = frozenset(
    "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
)


def new() -> str:
    """Return a random 128-bit identifier, hex encoded."""
    return secrets.token_hex(16)


def sanitize(raw: str | None) -> str:
    """Return raw when it is safe to propagate, and an empty string otherwise.

    An identifier arrives from the caller, so it is only accepted when it is
    short and alphanumeric: anything else could inject newlines into a log line.
    """
    if not raw or len(raw) > _MAX_LENGTH:
        return ""
    if not all(character in _ALLOWED for character in raw):
        return ""
    return raw
