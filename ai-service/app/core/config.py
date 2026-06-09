from functools import lru_cache

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Runtime configuration sourced from the environment.

    A missing `GEMINI_API_KEY` raises at construction, so the service refuses to
    start in a state where the first request would fail.
    """

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=False,
        extra="ignore",
    )

    gemini_api_key: str
    gemini_model: str = "gemini-2.5-flash"
    environment: str = "development"
    log_level: str = "INFO"
    max_cv_chars: int = 20_000
    # Shared secret the gateway must present. Empty disables the check (local dev).
    internal_api_key: str = ""

    @property
    def is_production(self) -> bool:
        return self.environment == "production"


@lru_cache
def get_settings() -> Settings:
    return Settings()
