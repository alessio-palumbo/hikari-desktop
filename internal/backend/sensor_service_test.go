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
			id: "sensaa-aabbccddeeff", name: "Bedroom radar", capabilities: []string{"presence", "target_count"}, targetCountMax: 3,
			connect: func(context.Context) (sensorClient, error) { return client, nil },
		}}, nil
	}, time.Hour, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close(context.Background())

	rssi := -58
	client.updates <- sensaa.Update{
		Sequence: 1, Presence: true, Targets: []sensaa.Target{{}, {}},
		Network: &sensaa.NetworkTelemetry{Transport: sensaa.NetworkTransportWiFi, RSSIDBm: &rssi},
	}
	waitForSensor(t, service, func(node SensorNode) bool {
		return node.Online && node.PresenceKnown && node.Present && node.TargetCount != nil && node.TargetCount.Known &&
			node.TargetCount.Value == 2 && node.TargetCount.Max == 3 && node.RSSIDBm != nil && *node.RSSIDBm == -58
	})

	client.errors <- errors.New("connection lost")
	waitForSensor(t, service, func(node SensorNode) bool {
		return !node.Online && !node.PresenceKnown && node.TargetCount != nil && !node.TargetCount.Known &&
			node.RSSIDBm == nil && node.ID == "sensaa-aabbccddeeff"
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

func TestSensorServiceEmitsRevisionedSnapshotsOnlyForPublicChanges(t *testing.T) {
	client := newTestSensorClient()
	service := newSensorService(func(context.Context) ([]sensorEndpoint, error) {
		return []sensorEndpoint{{
			id: "sensor", name: "Sensor", capabilities: []string{"presence"},
			connect: func(context.Context) (sensorClient, error) { return client, nil },
		}}, nil
	}, time.Hour, time.Second)
	events := make(chan SensorSnapshot, 8)
	service.SetSnapshotObserver(func(snapshot SensorSnapshot) { events <- snapshot })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close(context.Background())

	client.updates <- sensaa.Update{Sequence: 1, Presence: true}
	present := waitForSensorEvent(t, events, func(snapshot SensorSnapshot) bool {
		return len(snapshot.Nodes) == 1 && snapshot.Nodes[0].PresenceKnown && snapshot.Nodes[0].Present
	})
	client.updates <- sensaa.Update{Sequence: 2, Presence: true}
	client.updates <- sensaa.Update{Sequence: 3, Presence: false}
	clear := waitForSensorEvent(t, events, func(snapshot SensorSnapshot) bool {
		return len(snapshot.Nodes) == 1 && snapshot.Nodes[0].PresenceKnown && !snapshot.Nodes[0].Present
	})

	if clear.Revision != present.Revision+1 {
		t.Fatalf("clear revision = %d, want %d; duplicate update changed public state", clear.Revision, present.Revision+1)
	}
}

func TestSensorServicePreservesLatestStreamPositionWithoutEmittingDuplicateState(t *testing.T) {
	client := newTestSensorClient()
	service := newSensorService(func(context.Context) ([]sensorEndpoint, error) {
		return []sensorEndpoint{{
			id: "sensor", name: "Sensor", capabilities: []string{"presence"},
			connect: func(context.Context) (sensorClient, error) { return client, nil },
		}}, nil
	}, time.Hour, time.Second)
	events := make(chan SensorSnapshot, 8)
	service.SetSnapshotObserver(func(snapshot SensorSnapshot) { events <- snapshot })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close(context.Background())

	client.updates <- sensaa.Update{Sequence: 41, Uptime: 4 * time.Second, Presence: true}
	waitForSensorEvent(t, events, func(snapshot SensorSnapshot) bool {
		return len(snapshot.Nodes) == 1 && snapshot.Nodes[0].Present
	})
	client.updates <- sensaa.Update{Sequence: 42, Uptime: 4100 * time.Millisecond, Presence: true}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		service.mu.RLock()
		stream := service.nodes["sensor"].stream
		service.mu.RUnlock()
		if stream.hasUpdate && stream.sequence == 42 && stream.uptime == 4100*time.Millisecond {
			select {
			case duplicate := <-events:
				t.Fatalf("duplicate public state emitted snapshot %#v", duplicate)
			default:
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("latest Sensaa sequence and uptime were not retained")
}

func TestObserveSensorUpdateReportsGapsAndPeriodicTiming(t *testing.T) {
	startedAt := time.Unix(100, 0)
	state, first := observeSensorUpdate(sensorStreamState{}, sensaa.Update{Sequence: 10, Uptime: time.Second}, startedAt)
	if first.sequenceGap != 0 || first.summary != nil {
		t.Fatalf("first observation = %#v", first)
	}

	state, gap := observeSensorUpdate(state, sensaa.Update{Sequence: 12, Uptime: 1100 * time.Millisecond}, startedAt.Add(130*time.Millisecond))
	if gap.sequenceGap != 1 || gap.jitter != 30*time.Millisecond {
		t.Fatalf("gap observation = %#v", gap)
	}

	_, summary := observeSensorUpdate(state, sensaa.Update{Sequence: 13, Uptime: 11 * time.Second}, startedAt.Add(10130*time.Millisecond))
	if summary.summary == nil || summary.summary.samples != 2 || summary.summary.maximumJitter != 100*time.Millisecond {
		t.Fatalf("timing summary = %#v", summary.summary)
	}
}

func TestObserveSensorUpdateResetsTimingWindowAfterSensorRestart(t *testing.T) {
	startedAt := time.Unix(100, 0)
	state, _ := observeSensorUpdate(sensorStreamState{}, sensaa.Update{Sequence: 50, Uptime: 20 * time.Second}, startedAt)
	state, observation := observeSensorUpdate(state, sensaa.Update{Sequence: 1, Uptime: 100 * time.Millisecond}, startedAt.Add(time.Second))

	if !observation.streamReset {
		t.Fatalf("observation = %#v, want stream reset", observation)
	}
	if state.summarySamples != 0 || state.summaryStartedAt != startedAt.Add(time.Second) {
		t.Fatalf("state = %#v, want fresh summary window", state)
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

func waitForSensorEvent(t *testing.T, events <-chan SensorSnapshot, predicate func(SensorSnapshot) bool) SensorSnapshot {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case snapshot := <-events:
			if predicate(snapshot) {
				return snapshot
			}
		case <-timer.C:
			t.Fatal("sensor event did not arrive")
		}
	}
}
