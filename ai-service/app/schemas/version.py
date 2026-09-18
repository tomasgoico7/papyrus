from pydantic import BaseModel, ConfigDict
from pydantic.alias_generators import to_camel


class VersionResponse(BaseModel):
    """What the gateway needs to build a cache key it can trust."""

    model_config = ConfigDict(alias_generator=to_camel, populate_by_name=True)

    prompt_version: str
    model: str
