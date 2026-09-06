package backend

import (
	"context"
	"errors"
	"io"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/alessio-palumbo/sensaa"
)

const (
	defaultSensorDiscoveryInterval = 2 * time.Second
	defaultSensorStreamTimeout     = 5 * time.Second
)

type sensorClient interface {
	Read(context.Context) (sensaa.Update, error)
	Close() error
}

type sensorEndpoint struct {
	id           string
	name         string
	capabilities []string
	connect      func(context.Context) (sensorClient, error)
}

type sensorRuntime struct {
	node       SensorNode
	endpoint   sensorEndpoint
	connecting bool
}

// SensorService owns Sensaa discovery and stream reconnection for Hikari. It
// retains nodes after disconnect so frontend assignments remain meaningful.
type SensorService struct {
	mu                sync.RWMutex
	nodes             map[string]*sensorRuntime
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
	nodes := make([]SensorNode, 0, len(s.nodes))
	for _, runtime := range s.nodes {
		node := runtime.node
		node.Capabilities = append([]string(nil), node.Capabilities...)
		nodes = append(nodes, node)
	}
	s.mu.RUnlock()
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Name != nodes[j].Name {
			return nodes[i].Name < nodes[j].Name
		}
		return nodes[i].ID < nodes[j].ID
	})
	return SensorSnapshot{Nodes: nodes}, nil
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
		if runtime == nil {
			runtime = &sensorRuntime{}
			s.nodes[endpoint.id] = runtime
		}
		runtime.endpoint = endpoint
		runtime.node.ID = endpoint.id
		runtime.node.Name = endpoint.name
		runtime.node.Capabilities = append(runtime.node.Capabilities[:0], endpoint.capabilities...)
		startConnection := !runtime.connecting && !runtime.node.Online
		if startConnection {
			runtime.connecting = true
		}
		s.mu.Unlock()
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
	if runtime := s.nodes[endpoint.id]; runtime != nil {
		runtime.connecting = false
		runtime.node.Online = true
	}
	s.mu.Unlock()

	for {
		readCtx, cancel := context.WithTimeout(ctx, s.streamTimeout)
		update, err := client.Read(readCtx)
		cancel()
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
				log.Printf("hikari: Sensaa node %q disconnected: %v", endpoint.name, err)
			}
			s.markDisconnected(endpoint.id)
			return
		}
		s.mu.Lock()
		if runtime := s.nodes[endpoint.id]; runtime != nil {
			runtime.node.Online = true
			runtime.node.PresenceKnown = true
			runtime.node.Present = update.Presence
		}
		s.mu.Unlock()
	}
}

func (s *SensorService) markDisconnected(id string) {
	s.mu.Lock()
	if runtime := s.nodes[id]; runtime != nil {
		runtime.connecting = false
		runtime.node.Online = false
		runtime.node.PresenceKnown = false
	}
	s.mu.Unlock()
}

func discoverSensaaNodes(ctx context.Context) ([]sensorEndpoint, error) {
	nodes, err := sensaa.Discover(ctx)
	if err != nil {
		return nil, err
	}
	endpoints := make([]sensorEndpoint, 0, len(nodes))
	for _, node := range nodes {
		if !node.HasCapability(sensaa.CapabilityPresence) {
			continue
		}
		node := node
		capabilities := node.Capabilities()
		labels := make([]string, len(capabilities))
		for index, capability := range capabilities {
			labels[index] = string(capability)
		}
		endpoints = append(endpoints, sensorEndpoint{
			id:           node.ID(),
			name:         node.Name(),
			capabilities: labels,
			connect: func(connectCtx context.Context) (sensorClient, error) {
				return node.Connect(connectCtx)
			},
		})
	}
	return endpoints, nil
}
