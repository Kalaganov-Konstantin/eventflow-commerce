from pydantic import BaseModel
from pydantic_settings import BaseSettings, SettingsConfigDict


class ServerConfig(BaseModel):
    host: str = "0.0.0.0"
    port: int


class DatabaseConfig(BaseModel):
    url: str


class KafkaConfig(BaseModel):
    brokers: str = "localhost:9092"
    group_id: str = "notification-service"
    max_retries: int = 3


class SMTPConfig(BaseModel):
    host: str = ""
    port: int = 587
    user: str = ""
    password: str = ""
    from_address: str = "notifications@eventflow.commerce"
    use_tls: bool = True


class Config(BaseSettings):
    server: ServerConfig
    database: DatabaseConfig
    kafka: KafkaConfig = KafkaConfig()
    smtp: SMTPConfig = SMTPConfig()

    model_config = SettingsConfigDict(
        env_prefix="NOTIFICATION_",
        env_nested_delimiter="_",
        case_sensitive=False,
    )


def load_config() -> Config:
    """Load configuration from environment variables"""
    return Config()
