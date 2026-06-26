// Package test holds the order service's integration tests against a real postgres and kafka,
// started by `make test-deps-up` (see docker-compose.test.yml). This file carries no build tag so
// that `go test ./...` still finds a file to build in this package; the tests themselves are
// gated behind the integration tag.
package test

import (
	"context"
	"database/sql"
	stderrors "errors"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	_ "github.com/lib/pq" // registers the postgres driver
	"github.com/segmentio/kafka-go"
)

const (
	defaultTestDatabaseURL = "postgres://orders_user:orders_pass@localhost:5433/orders?sslmode=disable"
	defaultTestKafkaBroker = "localhost:9093"

	testConnectTimeout = 5 * time.Second
)

// testDatabaseURL returns the order integration test database URL, ORDER_TEST_DATABASE_URL
// overriding the default docker-compose.test.yml connection.
func testDatabaseURL() string {
	if v := os.Getenv("ORDER_TEST_DATABASE_URL"); v != "" {
		return v
	}
	return defaultTestDatabaseURL
}

// testKafkaBroker returns the integration test Kafka broker address, TEST_KAFKA_BROKER
// overriding the default docker-compose.test.yml listener.
func testKafkaBroker() string {
	if v := os.Getenv("TEST_KAFKA_BROKER"); v != "" {
		return v
	}
	return defaultTestKafkaBroker
}

// openTestDB opens a connection to the integration test database, verifies it is reachable and
// closes it when t ends. It fails the test immediately when the database cannot be reached, since
// these tests require `make test-deps-up` to have already run.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("postgres", testDatabaseURL())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	db.SetMaxOpenConns(10)

	ctx, cancel := context.WithTimeout(context.Background(), testConnectTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("order integration test database not reachable at %s, run `make test-deps-up` first: %v", testDatabaseURL(), err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

// ensureTopic makes sure topic exists on the test broker before a test publishes to or subscribes
// from it. Kafka auto-creates topics on first access, but a produce request that arrives before
// creation finishes still fails with "unknown topic or partition", so tests create it explicitly
// and up front instead of racing that window.
func ensureTopic(t *testing.T, topic string) {
	t.Helper()

	conn, err := kafka.Dial("tcp", testKafkaBroker())
	if err != nil {
		t.Fatalf("dial kafka broker: %v", err)
	}
	defer func() { _ = conn.Close() }()

	controller, err := conn.Controller()
	if err != nil {
		t.Fatalf("find kafka controller: %v", err)
	}

	controllerConn, err := kafka.Dial("tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		t.Fatalf("dial kafka controller: %v", err)
	}
	defer func() { _ = controllerConn.Close() }()

	err = controllerConn.CreateTopics(kafka.TopicConfig{
		Topic:             topic,
		NumPartitions:     1,
		ReplicationFactor: 1,
	})
	if err != nil && !stderrors.Is(err, kafka.TopicAlreadyExists) {
		t.Fatalf("create topic %s: %v", topic, err)
	}

	// CreateTopics returning success only means the controller accepted the request, not that the
	// topic's metadata has propagated to the broker handling produce requests yet; a message
	// published too soon after still fails with "unknown topic or partition".
	deadline := time.Now().Add(10 * time.Second)
	for {
		if partitionsExist(topic) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("topic %s did not become ready in time", topic)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func partitionsExist(topic string) bool {
	conn, err := kafka.Dial("tcp", testKafkaBroker())
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()

	partitions, err := conn.ReadPartitions(topic)
	return err == nil && len(partitions) > 0
}
