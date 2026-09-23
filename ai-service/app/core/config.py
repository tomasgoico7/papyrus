from functools import lru_cache

from pydantic import SecretStr
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=False,
        extra="ignore",
    )

    gemini_api_key: SecretStr
    gemini_model: str = "gemini-2.5-flash"
    gemini_temperature: float = 0.2
    gemini_timeout: int = 45
    environment: str = "development"
    log_level: str = "INFO"
    max_cv_chars: int = 20_000
    internal_api_key: str = ""  # empty disables auth check (local dev)
    # Setting an endpoint is what turns tracing on. The exporter reads its own
    # headers and protocol from the other standard OTEL_* variables, which is
    # what a backend's setup instructions hand you.
    otel_exporter_otlp_endpoint: str = ""
    # Share of traces recorded when nothing upstream has already decided. In
    # practice the gateway decides first and this rarely applies.
    trace_sample_ratio: float = 1.0

    @property
    def is_production(self) -> bool:
        return self.environment == "production"


@lru_cache
def get_settings() -> Settings:
    # Every field is populated from the environment, which the type checker
    # cannot see, so it reads the call as missing required arguments.
    return Settings()  # type: ignore[call-arg]
