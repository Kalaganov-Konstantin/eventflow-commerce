from pydantic import BaseModel
from pydantic_settings import BaseSettings


class ServerConfig(BaseModel):
    host: str = "0.0.0.0"
    port: int


class Config(BaseSettings):
    server: ServerConfig

    class Config:
        env_prefix = "NOTIFICATION_"
        env_nested_delimiter = "_"
        case_sensitive = False


def load_config() -> Config:
    """Load configuration from environment variables"""
    return Config()
