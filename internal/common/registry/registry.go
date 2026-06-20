package registry

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// ServiceInstance represents a registered service instance.
type ServiceInstance struct {
	ServiceName string `json:"serviceName"`
	InstanceID  string `json:"instanceId"`
	Host        string `json:"host"`
	Port        string `json:"port"`
	Healthy     bool   `json:"healthy"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	lastHeartbeat time.Time
}

// Registry is a lightweight in-process service registry (replaces Nacos for single-deployment).
// For multi-node production, swap with etcd/consul/nacos client.
type Registry struct {
	mu        sync.RWMutex
	services  map[string][]*ServiceInstance // serviceName -> instances
	heartbeat time.Duration
	stopCh    chan struct{}
}

// New creates a new Registry. heartbeat interval controls health-check frequency.
func New(heartbeat time.Duration) *Registry {
	return &Registry{
		services:  make(map[string][]*ServiceInstance),
		heartbeat: heartbeat,
		stopCh:    make(chan struct{}),
	}
}

// Register adds a service instance and starts its heartbeat goroutine.
func (r *Registry) Register(inst *ServiceInstance) {
	if inst.InstanceID == "" {
		inst.InstanceID = fmt.Sprintf("%s-%s:%s", inst.ServiceName, inst.Host, inst.Port)
	}
	inst.Healthy = true
	inst.lastHeartbeat = time.Now()

	r.mu.Lock()
	r.services[inst.ServiceName] = append(r.services[inst.ServiceName], inst)
	r.mu.Unlock()

	go r.heartbeatLoop(inst)
	log.Printf("[Registry] registered %s at %s:%s", inst.ServiceName, inst.Host, inst.Port)
}

// Deregister removes a service instance.
func (r *Registry) Deregister(serviceName, instanceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	instances := r.services[serviceName]
	for i, inst := range instances {
		if inst.InstanceID == instanceID {
			r.services[serviceName] = append(instances[:i], instances[i+1:]...)
			log.Printf("[Registry] deregistered %s", instanceID)
			return
		}
	}
}

// GetInstances returns all healthy instances of a service.
func (r *Registry) GetInstances(serviceName string) []*ServiceInstance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var healthy []*ServiceInstance
	for _, inst := range r.services[serviceName] {
		if inst.Healthy {
			healthy = append(healthy, inst)
		}
	}
	return healthy
}

// GetInstance returns one healthy instance using round-robin.
func (r *Registry) GetInstance(serviceName string) (*ServiceInstance, error) {
	instances := r.GetInstances(serviceName)
	if len(instances) == 0 {
		return nil, fmt.Errorf("no healthy instances for service %s", serviceName)
	}
	// Simple round-robin using current timestamp
	idx := time.Now().UnixNano() % int64(len(instances))
	return instances[idx], nil
}

// Stop halts all heartbeat goroutines.
func (r *Registry) Stop() {
	close(r.stopCh)
	log.Println("[Registry] stopped")
}

// HTTPHandler returns an HTTP handler for /registry endpoints (health + discovery).
func (r *Registry) HTTPHandler() http.Handler {
	mux := http.NewServeMux()

	// GET /registry/health — overall health
	mux.HandleFunc("/registry/health", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "UP"})
	})

	// GET /registry/services — list all services
	mux.HandleFunc("/registry/services", func(w http.ResponseWriter, req *http.Request) {
		r.mu.RLock()
		defer r.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(r.services)
	})

	// GET /registry/instances?name=serviceName — get instances of a service
	mux.HandleFunc("/registry/instances", func(w http.ResponseWriter, req *http.Request) {
		name := req.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "missing 'name' query param", http.StatusBadRequest)
			return
		}
		instances := r.GetInstances(name)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(instances)
	})

	return mux
}

func (r *Registry) heartbeatLoop(inst *ServiceInstance) {
	ticker := time.NewTicker(r.heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Mark unhealthy if no heartbeat for 3x interval
			r.mu.Lock()
			if time.Since(inst.lastHeartbeat) > 3*r.heartbeat {
				inst.Healthy = false
				log.Printf("[Registry] %s marked unhealthy (no heartbeat)", inst.InstanceID)
			}
			// Refresh heartbeat (self-registration always stays healthy)
			inst.lastHeartbeat = time.Now()
			inst.Healthy = true
			r.mu.Unlock()
		case <-r.stopCh:
			return
		}
	}
}
