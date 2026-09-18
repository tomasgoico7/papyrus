import logging
import sys

import structlog
from structlog.typing import Processor


def configure_logging(level: str, *, json_output: bool) -> None:
    """Route structlog and the standard library through one renderer.

    The services log through `logging`, uvicorn logs through `logging`, and the
    request middleware binds context variables. Funnelling all of it through a
    single `ProcessorFormatter` is what makes every line — including an
    unexpected traceback from the model provider — carry the request id.
    """
    shared: list[Processor] = [
        structlog.contextvars.merge_contextvars,
        structlog.processors.add_log_level,
        structlog.processors.TimeStamper(fmt="iso", utc=True),
        structlog.processors.StackInfoRenderer(),
        structlog.processors.format_exc_info,
    ]
    renderer: Processor = (
        structlog.processors.JSONRenderer()
        if json_output
        else structlog.dev.ConsoleRenderer(colors=False)
    )

    structlog.configure(
        processors=[*shared, structlog.stdlib.ProcessorFormatter.wrap_for_formatter],
        logger_factory=structlog.stdlib.LoggerFactory(),
        cache_logger_on_first_use=True,
    )

    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(
        structlog.stdlib.ProcessorFormatter(
            foreign_pre_chain=shared,
            processors=[
                structlog.stdlib.ProcessorFormatter.remove_processors_meta,
                renderer,
            ],
        )
    )

    root = logging.getLogger()
    root.handlers = [handler]
    root.setLevel(_level(level))

    # uvicorn installs handlers of its own at startup; clearing them and letting
    # the records propagate keeps its access log in the same format as ours.
    for name in ("uvicorn", "uvicorn.access", "uvicorn.error"):
        server_logger = logging.getLogger(name)
        server_logger.handlers = []
        server_logger.propagate = True


def _level(level: str) -> int:
    """Resolve a level name, falling back to INFO rather than silencing logs."""
    resolved = logging.getLevelName(level.strip().upper())
    return resolved if isinstance(resolved, int) else logging.INFO
