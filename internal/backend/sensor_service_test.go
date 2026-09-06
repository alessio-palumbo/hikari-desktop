package backend

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alessio-palumbo/sensaa"
)

type testSensorClient struct {
	updates chan sensaa.Update
	errors  chan error
	closed  chan struct{}
	once    sync.Once
}

func newTestSensorClient() *testSensorClient {
	return &testSensorClient{
		updates: make(chan sensaa.Update, 2),
		errors:  make(chan error, 1),
		closed:  make(chan struct{}),
	}
}

func (c *testSensorClient) Read(ctx context.Context) (sensaa.Update, error) {
	select {
	case update := <-c.updates:
		return update, nil
	case err := <-c.errors:
		return sensaa.Update{}, err
	case <-c.closed:
		return sensaa.Update{}, errors.New("closed")
	case <-ctx.Done():
		return sensaa.Update{}, ctx.Err()
	}
}

func (c *testSensorClient) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func TestSensorServiceTracksPresenceAndRetainsOfflineNode(t *testing.T) {
	client := newTestSensorClient()
	service := newSensorService(func(context.Context) ([]sensorEndpoint, error) {
		return []sensorEndpoint{{
			id: "sensaa-aabbccddeeff", name: "Bedroom radar", capabilities: []string{"presence"},
			connect: func(context.Context) (sensorClient, error) { return client, nil },
		}}, nil
	}, time.Hour, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close(context.Background())

	client.updates <- sensaa.Update{Sequence: 1, Presence: true}
	waitForSensor(t, service, func(node SensorNode) bool {
		return node.Online && node.PresenceKnown && node.Present
	})

	client.errors <- errors.New("connection lost")
	waitForSensor(t, service, func(node SensorNode) bool {
		return !node.Online && !node.PresenceKnown && node.ID == "sensaa-aabbccddeeff"
	})
}

func TestSensorServiceReconnectsKnownNodeByStableID(t *testing.T) {
	first := newTestSensorClient()
	second := newTestSensorClient()
	clients := []sensorClient{first, second}
	connects := 0
	service := newSensorService(func(context.Context) ([]sensorEndpoint, error) {
		return []sensorEndpoint{{
			id: "stable-id", name: "Renamed sensor", capabilities: []string{"presence"},
			connect: func(context.Context) (sensorClient, error) {
				client := clients[min(connects, len(clients)-1)]
				connects++
				return client, nil
			},
		}}, nil
	}, time.Hour, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close(context.Background())

	first.updates <- sensaa.Update{Sequence: 1, Presence: true}
	waitForSensor(t, service, func(node SensorNode) bool { return node.Online && node.PresenceKnown })
	first.errors <- errors.New("restart")
	waitForSensor(t, service, func(node SensorNode) bool { return !node.Online })
	service.discoverOnce(ctx)
	second.updates <- sensaa.Update{Sequence: 2, Presence: false}
	waitForSensor(t, service, func(node SensorNode) bool {
		return node.Online && node.PresenceKnown && !node.Present && node.Name == "Renamed sensor"
	})
	if connects < 2 {
		t.Fatalf("connect calls = %d, want at least 2", connects)
	}
}

func TestSensorSnapshotIsSortedAndDefensivelyCopied(t *testing.T) {
	service := newSensorService(nil, time.Hour, time.Second)
	service.nodes["b"] = &sensorRuntime{node: SensorNode{ID: "b", Name: "Zulu", Capabilities: []string{"presence"}}}
	service.nodes["a"] = &sensorRuntime{node: SensorNode{ID: "a", Name: "Alpha", Capabilities: []string{"presence"}}}

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Nodes) != 2 || snapshot.Nodes[0].ID != "a" {
		t.Fatalf("Snapshot returned %#v", snapshot)
	}
	snapshot.Nodes[0].Capabilities[0] = "changed"
	if service.nodes["a"].node.Capabilities[0] == "changed" {
		t.Fatal("Snapshot returned service-owned capability storage")
	}
}

func waitForSensor(t *testing.T, service *SensorService, predicate func(SensorNode) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, _ := service.Snapshot(context.Background())
		if len(snapshot.Nodes) == 1 && predicate(snapshot.Nodes[0]) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	snapshot, _ := service.Snapshot(context.Background())
	t.Fatalf("sensor state did not settle: %#v", snapshot)
}
