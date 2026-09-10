package backend

import (
	"context"
	"errors"
	"io"
	"log"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/alessio-palumbo/sensaa"
)

const (
	defaultSensorDiscoveryInterval = 2 * time.Second
	defaultSensorStreamTimeout     = 5 * time.Second
	sensorTimingSummaryInterval    = 10 * time.Second
	sensorRSSIPublishInterval      = 10 * time.Second
)

type sensorClient interface {
	Read(context.Context) (sensaa.Update, error)
	Close() error
}

type sensorEndpoint struct {
	id             string
	name           string
	capabilities   []string
	targetCountMax int
	connect        func(context.Context) (sensorClient, error)
}

type sensorRuntime struct {
	node       SensorNode
	endpoint   sensorEndpoint
	connecting bool
	stream     sensorStreamState
	rssiAt     time.Time
}

// sensorStreamState retains protocol timing data for diagnostics without
// exposing transport details through Hikari's public sensor model.
type sensorStreamState struct {
	sequence             uint64
	uptime               time.Duration
	receivedAt           time.Time
	hasUpdate            bool
	summaryStartedAt     time.Time
	summarySamples       uint64
	receiveIntervalTotal time.Duration
	uptimeIntervalTotal  time.Duration
	jitterTotal          time.Duration
	maximumJitter        time.Duration
}

type sensorStreamObservation struct {
	previousSequence uint64
	previousUptime   time.Duration
	receiveInterval  time.Duration
	uptimeInterval   time.Duration
	jitter           time.Duration
	sequenceGap      uint64
	streamReset      bool
	summary          *sensorTimingSummary
}

type sensorTimingSummary struct {
	samples                uint64
	averageReceiveInterval time.Duration
	averageUptimeInterval  time.Duration
	averageJitter          time.Duration
	maximumJitter          time.Duration
}

// SensorService owns Sensaa discovery and stream reconnection for Hikari. It
// retains nodes after disconnect so frontend assignments remain meaningful.
type SensorService struct {
	mu                sync.RWMutex
	nodes             map[string]*sensorRuntime
	revision          uint64
	observer          func(SensorSnapshot)
	discover          func(context.Context) ([]sensorEndpoint, error)
	discoveryInterval time.Duration
	streamTimeout     time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewSensorService() *SensorService {
	return newSensorService(discoverSensaaNodes, defaultSensorDiscoveryInterval, defaultSensorStreamTimeout)
}

func newSensorService(
	discover func(context.Context) ([]sensorEndpoint, error),
	discoveryInterval time.Duration,
	streamTimeout time.Duration,
) *SensorService {
	return &SensorService{
		nodes:             make(map[string]*sensorRuntime),
		discover:          discover,
		discoveryInterval: discoveryInterval,
		streamTimeout:     streamTimeout,
	}
}

func (s *SensorService) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return nil
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	runCtx := s.ctx
	s.mu.Unlock()

	s.wg.Add(1)
	go s.discoveryLoop(runCtx)
	return nil
}

func (s *SensorService) Close(context.Context) error {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.ctx = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		s.wg.Wait()
	}
	return nil
}

func (s *SensorService) Snapshot(context.Context) (SensorSnapshot, error) {
	s.mu.RLock()
	snapshot := s.snapshotLocked()
	s.mu.RUnlock()
	return snapshot, nil
}

// SetSnapshotObserver registers the UI delivery boundary. SensorService stays
// independent of Wails; App translates these snapshots into runtime events.
func (s *SensorService) SetSnapshotObserver(observer func(SensorSnapshot)) {
	s.mu.Lock()
	s.observer = observer
	s.mu.Unlock()
}

func (s *SensorService) snapshotLocked() SensorSnapshot {
	nodes := make([]SensorNode, 0, len(s.nodes))
	for _, runtime := range s.nodes {
		nodes = append(nodes, cloneSensorNode(runtime.node))
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Name != nodes[j].Name {
			return nodes[i].Name < nodes[j].Name
		}
		return nodes[i].ID < nodes[j].ID
	})
	return SensorSnapshot{Revision: s.revision, Nodes: nodes}
}

func (s *SensorService) changedSnapshotLocked() (func(SensorSnapshot), SensorSnapshot) {
	s.revision++
	return s.observer, s.snapshotLocked()
}

func notifySensorObserver(observer func(SensorSnapshot), snapshot SensorSnapshot) {
	if observer != nil {
		observer(snapshot)
	}
}

func (s *SensorService) discoveryLoop(ctx context.Context) {
	defer s.wg.Done()
	for {
		s.discoverOnce(ctx)
		timer := time.NewTimer(s.discoveryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *SensorService) discoverOnce(ctx context.Context) {
	endpoints, err := s.discover(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("hikari: Sensaa discovery failed: %v", err)
		return
	}
	for _, endpoint := range endpoints {
		if endpoint.id == "" || endpoint.connect == nil {
			continue
		}
		s.mu.Lock()
		runtime := s.nodes[endpoint.id]
		newNode := runtime == nil
		if runtime == nil {
			runtime = &sensorRuntime{}
			s.nodes[endpoint.id] = runtime
		}
		previous := cloneSensorNode(runtime.node)
		runtime.endpoint = endpoint
		runtime.node.ID = endpoint.id
		runtime.node.Name = endpoint.name
		runtime.node.Capabilities = append(runtime.node.Capabilities[:0], endpoint.capabilities...)
		if slices.Contains(endpoint.capabilities, string(sensaa.CapabilityTargetCount)) {
			if runtime.node.TargetCount == nil {
				runtime.node.TargetCount = &SensorTargetCount{}
			}
			runtime.node.TargetCount.Max = endpoint.targetCountMax
		} else {
			runtime.node.TargetCount = nil
		}
		startConnection := !runtime.connecting && !runtime.node.Online
		if startConnection {
			runtime.connecting = true
		}
		var observer func(SensorSnapshot)
		var snapshot SensorSnapshot
		if newNode || !sensorNodesEqual(previous, runtime.node) {
			observer, snapshot = s.changedSnapshotLocked()
		}
		s.mu.Unlock()
		notifySensorObserver(observer, snapshot)
		if startConnection {
			s.wg.Add(1)
			go s.consume(ctx, endpoint)
		}
	}
}

func (s *SensorService) consume(ctx context.Context, endpoint sensorEndpoint) {
	defer s.wg.Done()
	client, err := endpoint.connect(ctx)
	if err != nil {
		s.markDisconnected(endpoint.id)
		return
	}
	defer client.Close()

	s.mu.Lock()
	var observer func(SensorSnapshot)
	var snapshot SensorSnapshot
	if runtime := s.nodes[endpoint.id]; runtime != nil {
		previous := cloneSensorNode(runtime.node)
		runtime.connecting = false
		runtime.node.Online = true
		if !sensorNodesEqual(previous, runtime.node) {
			observer, snapshot = s.changedSnapshotLocked()
		}
	}
	s.mu.Unlock()
	notifySensorObserver(observer, snapshot)

	for {
		readCtx, cancel := context.WithTimeout(ctx, s.streamTimeout)
		update, err := client.Read(readCtx)
		cancel()
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
				log.Printf("hikari: Sensaa node %q disconnected: %v", endpoint.name, err)
			}
			s.logSensorDisconnect(endpoint.id, endpoint.name, err)
			s.markDisconnected(endpoint.id)
			return
		}
		receivedAt := time.Now()
		s.mu.Lock()
		observer = nil
		snapshot = SensorSnapshot{}
		var previousNode, currentNode SensorNode
		var observation sensorStreamObservation
		observed := false
		if runtime := s.nodes[endpoint.id]; runtime != nil {
			previousNode = cloneSensorNode(runtime.node)
			runtime.stream, observation = observeSensorUpdate(runtime.stream, update, receivedAt)
			runtime.node.Online = true
			runtime.node.PresenceKnown = true
			runtime.node.Present = update.Presence
			if update.Network != nil && update.Network.RSSIDBm != nil &&
				(runtime.node.RSSIDBm == nil || receivedAt.Sub(runtime.rssiAt) >= sensorRSSIPublishInterval) {
				rssi := *update.Network.RSSIDBm
				runtime.node.RSSIDBm = &rssi
				runtime.rssiAt = receivedAt
			}
			if slices.Contains(endpoint.capabilities, string(sensaa.CapabilityTargetCount)) {
				if runtime.node.TargetCount == nil {
					runtime.node.TargetCount = &SensorTargetCount{Max: endpoint.targetCountMax}
				}
				runtime.node.TargetCount.Known = true
				runtime.node.TargetCount.Value = update.TargetCount()
			}
			if !sensorNodesEqual(previousNode, runtime.node) {
				observer, snapshot = s.changedSnapshotLocked()
			}
			currentNode = cloneSensorNode(runtime.node)
			observed = true
		}
		s.mu.Unlock()
		if observed {
			logSensorUpdate(endpoint.id, endpoint.name, previousNode, currentNode, update, receivedAt, observation)
		}
		notifySensorObserver(observer, snapshot)
	}
}

func observeSensorUpdate(previous sensorStreamState, update sensaa.Update, receivedAt time.Time) (sensorStreamState, sensorStreamObservation) {
	next := previous
	observation := sensorStreamObservation{
		previousSequence: previous.sequence,
		previousUptime:   previous.uptime,
	}
	next.sequence = update.Sequence
	next.uptime = update.Uptime
	next.receivedAt = receivedAt
	next.hasUpdate = true
	if !previous.hasUpdate {
		next.summaryStartedAt = receivedAt
		return next, observation
	}

	observation.receiveInterval = receivedAt.Sub(previous.receivedAt)
	observation.uptimeInterval = update.Uptime - previous.uptime
	observation.jitter = observation.receiveInterval - observation.uptimeInterval
	if update.Sequence <= previous.sequence || update.Uptime < previous.uptime {
		observation.streamReset = true
	} else if update.Sequence != previous.sequence+1 {
		observation.sequenceGap = update.Sequence - previous.sequence - 1
	}
	if observation.streamReset {
		next.summaryStartedAt = receivedAt
		next.summarySamples = 0
		next.receiveIntervalTotal = 0
		next.uptimeIntervalTotal = 0
		next.jitterTotal = 0
		next.maximumJitter = 0
		return next, observation
	}

	next.summarySamples++
	next.receiveIntervalTotal += observation.receiveInterval
	next.uptimeIntervalTotal += observation.uptimeInterval
	next.jitterTotal += observation.jitter
	if observation.jitter > next.maximumJitter {
		next.maximumJitter = observation.jitter
	}
	if receivedAt.Sub(next.summaryStartedAt) >= sensorTimingSummaryInterval && next.summarySamples > 0 {
		samples := time.Duration(next.summarySamples)
		observation.summary = &sensorTimingSummary{
			samples:                next.summarySamples,
			averageReceiveInterval: next.receiveIntervalTotal / samples,
			averageUptimeInterval:  next.uptimeIntervalTotal / samples,
			averageJitter:          next.jitterTotal / samples,
			maximumJitter:          next.maximumJitter,
		}
		next.summaryStartedAt = receivedAt
		next.summarySamples = 0
		next.receiveIntervalTotal = 0
		next.uptimeIntervalTotal = 0
		next.jitterTotal = 0
		next.maximumJitter = 0
	}
	return next, observation
}

func logSensorUpdate(id, name string, previous, current SensorNode, update sensaa.Update, receivedAt time.Time, observation sensorStreamObservation) {
	if hikariTraceEnabled() {
		log.Printf(
			"hikari: Sensaa receive at=%s id=%q name=%q sequence=%d uptime_ms=%d receive_interval_ms=%.3f sensor_interval_ms=%.3f network_jitter_ms=%.3f presence=%v targets=%d",
			receivedAt.Format(time.RFC3339Nano), id, name, update.Sequence, update.Uptime.Milliseconds(),
			milliseconds(observation.receiveInterval), milliseconds(observation.uptimeInterval), milliseconds(observation.jitter),
			update.Presence, update.TargetCount(),
		)
	}
	if !hikariDebugEnabled() {
		return
	}
	if !previous.PresenceKnown || previous.Present != current.Present {
		log.Printf("hikari: Sensaa presence at=%s id=%q name=%q sequence=%d occupied=%v", receivedAt.Format(time.RFC3339Nano), id, name, update.Sequence, current.Present)
	}
	if observation.streamReset {
		log.Printf(
			"hikari: Sensaa stream reset id=%q name=%q previous_sequence=%d sequence=%d previous_uptime_ms=%d uptime_ms=%d",
			id, name, observation.previousSequence, update.Sequence, observation.previousUptime.Milliseconds(), update.Uptime.Milliseconds(),
		)
	} else if observation.sequenceGap > 0 {
		log.Printf(
			"hikari: Sensaa sequence gap id=%q name=%q previous=%d current=%d missing=%d",
			id, name, observation.previousSequence, update.Sequence, observation.sequenceGap,
		)
	}
	if summary := observation.summary; summary != nil {
		log.Printf(
			"hikari: Sensaa timing id=%q name=%q samples=%d receive_interval_avg_ms=%.3f sensor_interval_avg_ms=%.3f network_jitter_avg_ms=%.3f network_jitter_max_ms=%.3f",
			id, name, summary.samples, milliseconds(summary.averageReceiveInterval), milliseconds(summary.averageUptimeInterval),
			milliseconds(summary.averageJitter), milliseconds(summary.maximumJitter),
		)
	}
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func (s *SensorService) logSensorDisconnect(id, name string, err error) {
	if !hikariDebugEnabled() {
		return
	}
	s.mu.RLock()
	stream := sensorStreamState{}
	if runtime := s.nodes[id]; runtime != nil {
		stream = runtime.stream
	}
	s.mu.RUnlock()
	log.Printf(
		"hikari: Sensaa disconnect at=%s id=%q name=%q last_sequence=%d last_uptime_ms=%d error=%q",
		time.Now().Format(time.RFC3339Nano), id, name, stream.sequence, stream.uptime.Milliseconds(), err,
	)
}

func (s *SensorService) markDisconnected(id string) {
	s.mu.Lock()
	var observer func(SensorSnapshot)
	var snapshot SensorSnapshot
	if runtime := s.nodes[id]; runtime != nil {
		previous := cloneSensorNode(runtime.node)
		runtime.connecting = false
		runtime.node.Online = false
		runtime.node.PresenceKnown = false
		runtime.node.RSSIDBm = nil
		if runtime.node.TargetCount != nil {
			runtime.node.TargetCount.Known = false
		}
		if !sensorNodesEqual(previous, runtime.node) {
			observer, snapshot = s.changedSnapshotLocked()
		}
	}
	s.mu.Unlock()
	notifySensorObserver(observer, snapshot)
}

func sensorNodesEqual(left, right SensorNode) bool {
	if left.ID != right.ID || left.Name != right.Name || left.Online != right.Online ||
		left.PresenceKnown != right.PresenceKnown || left.Present != right.Present ||
		!slices.Equal(left.Capabilities, right.Capabilities) {
		return false
	}
	if left.RSSIDBm == nil || right.RSSIDBm == nil {
		if left.RSSIDBm != nil || right.RSSIDBm != nil {
			return false
		}
	} else if *left.RSSIDBm != *right.RSSIDBm {
		return false
	}
	if left.TargetCount == nil || right.TargetCount == nil {
		return left.TargetCount == nil && right.TargetCount == nil
	}
	return *left.TargetCount == *right.TargetCount
}

func cloneSensorNode(node SensorNode) SensorNode {
	node.Capabilities = append([]string(nil), node.Capabilities...)
	if node.TargetCount != nil {
		targetCount := *node.TargetCount
		node.TargetCount = &targetCount
	}
	if node.RSSIDBm != nil {
		rssi := *node.RSSIDBm
		node.RSSIDBm = &rssi
	}
	return node
}

func discoverSensaaNodes(ctx context.Context) ([]sensorEndpoint, error) {
	nodes, err := sensaa.Discover(ctx)
	if err != nil {
		return nil, err
	}
	endpoints := make([]sensorEndpoint, 0, len(nodes))
	for _, node := range nodes {
		node := node
		capabilities := node.Capabilities()
		labels := make([]string, len(capabilities))
		for index, capability := range capabilities {
			labels[index] = string(capability)
		}
		var targetCountMax int
		if targetCount, ok := node.TargetCountCapability(); ok {
			targetCountMax = targetCount.Max
		}
		endpoints = append(endpoints, sensorEndpoint{
			id:             node.ID(),
			name:           node.Name(),
			capabilities:   labels,
			targetCountMax: targetCountMax,
			connect: func(connectCtx context.Context) (sensorClient, error) {
				return node.Connect(connectCtx)
			},
		})
	}
	return endpoints, nil
}
