import pytest

from notification.config.config import Config


@pytest.fixture
def env(monkeypatch):
    monkeypatch.setenv("NOTIFICATION_SERVER_PORT", "8084")
    monkeypatch.setenv(
        "NOTIFICATION_DATABASE_URL",
        "postgres://notifications_user:notifications_pass@localhost:5432/notifications",
    )
    monkeypatch.setenv("NOTIFICATION_KAFKA_BROKERS", "kafka:9092,kafka2:9092")
    monkeypatch.setenv("NOTIFICATION_KAFKA_GROUP_ID", "notification-service")
    monkeypatch.setenv("NOTIFICATION_SMTP_HOST", "smtp.example.com")
    monkeypatch.setenv("NOTIFICATION_SMTP_PORT", "2525")
    monkeypatch.setenv("NOTIFICATION_SMTP_USER", "smtp-user")
    monkeypatch.setenv("NOTIFICATION_SMTP_PASSWORD", "smtp-pass")


def test_config_reads_nested_env_vars(env):
    config = Config()

    assert config.server.host == "0.0.0.0"
    assert config.server.port == 8084
    assert (
        config.database.url
        == "postgres://notifications_user:notifications_pass@localhost:5432/notifications"
    )
    assert config.kafka.brokers == "kafka:9092,kafka2:9092"
    assert config.kafka.group_id == "notification-service"
    assert config.smtp.host == "smtp.example.com"
    assert config.smtp.port == 2525
    assert config.smtp.user == "smtp-user"
    assert config.smtp.password == "smtp-pass"


def test_config_defaults_kafka_and_smtp(env, monkeypatch):
    monkeypatch.delenv("NOTIFICATION_KAFKA_GROUP_ID", raising=False)
    monkeypatch.delenv("NOTIFICATION_SMTP_HOST", raising=False)

    config = Config()

    assert config.kafka.group_id == "notification-service"
    assert config.smtp.host == ""
