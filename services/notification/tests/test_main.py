from fastapi.testclient import TestClient
from notification.main import app

client = TestClient(app)


def test_health_check_endpoint():
    """
    Tests that the /health endpoint returns a 200 OK status
    and the expected response body.
    """
    response = client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "healthy"}
