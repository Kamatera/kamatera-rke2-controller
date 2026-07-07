package controller

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type KamateraServer struct {
	Name       string
	Datacenter string
	Power      string
}

type ServerFilter struct {
	Datacenters map[string]struct{}
	NameGlob    string
}

func NewServerFilter(datacentersCSV string, nameGlob string) (ServerFilter, error) {
	filter := ServerFilter{Datacenters: map[string]struct{}{}, NameGlob: strings.TrimSpace(nameGlob)}
	for _, datacenter := range strings.Split(datacentersCSV, ",") {
		datacenter = strings.TrimSpace(datacenter)
		if datacenter == "" {
			continue
		}
		filter.Datacenters[datacenter] = struct{}{}
	}
	if filter.NameGlob != "" {
		if _, err := filepath.Match(filter.NameGlob, ""); err != nil {
			return ServerFilter{}, err
		}
	}
	return filter, nil
}

func (f ServerFilter) Match(server KamateraServer) bool {
	if len(f.Datacenters) > 0 {
		if _, ok := f.Datacenters[server.Datacenter]; !ok {
			return false
		}
	}
	if f.NameGlob != "" {
		matched, err := filepath.Match(f.NameGlob, server.Name)
		if err != nil || !matched {
			return false
		}
	}
	return true
}

type ServerStateStore struct {
	mu          sync.RWMutex
	initialized bool
	servers     map[string]KamateraServer
}

type ServerStateDiff struct {
	Initial      bool
	Current      []KamateraServer
	Added        []KamateraServer
	Removed      []KamateraServer
	PowerChanged []ServerPowerChange
}

type ServerPowerChange struct {
	Name       string
	Datacenter string
	OldPower   string
	NewPower   string
}

func NewServerStateStore() *ServerStateStore {
	return &ServerStateStore{servers: map[string]KamateraServer{}}
}

func (s *ServerStateStore) Replace(servers []KamateraServer) ServerStateDiff {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := map[string]KamateraServer{}
	for _, server := range servers {
		next[serverStateKey(server)] = server
	}

	diff := ServerStateDiff{Initial: !s.initialized, Current: sortedServers(next)}
	if s.initialized {
		for key, server := range next {
			previous, ok := s.servers[key]
			if !ok {
				diff.Added = append(diff.Added, server)
				continue
			}
			if previous.Power != server.Power {
				diff.PowerChanged = append(diff.PowerChanged, ServerPowerChange{Name: server.Name, Datacenter: server.Datacenter, OldPower: previous.Power, NewPower: server.Power})
			}
		}
		for name, server := range s.servers {
			if _, ok := next[name]; !ok {
				diff.Removed = append(diff.Removed, server)
			}
		}
	}

	s.initialized = true
	s.servers = next
	sortServers(diff.Added)
	sortServers(diff.Removed)
	sort.Slice(diff.PowerChanged, func(i, j int) bool { return diff.PowerChanged[i].Name < diff.PowerChanged[j].Name })
	return diff
}

func (s *ServerStateStore) Get(name string) (KamateraServer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var matched KamateraServer
	matches := 0
	for _, server := range s.servers {
		if server.Name != name {
			continue
		}
		matched = server
		matches++
	}
	return matched, matches == 1
}

func (s *ServerStateStore) List() []KamateraServer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedServers(s.servers)
}

func sortedServers(servers map[string]KamateraServer) []KamateraServer {
	items := make([]KamateraServer, 0, len(servers))
	for _, server := range servers {
		items = append(items, server)
	}
	sortServers(items)
	return items
}

func sortServers(servers []KamateraServer) {
	sort.Slice(servers, func(i, j int) bool {
		if servers[i].Name == servers[j].Name {
			return servers[i].Datacenter < servers[j].Datacenter
		}
		return servers[i].Name < servers[j].Name
	})
}

func serverStateKey(server KamateraServer) string {
	return server.Datacenter + "/" + server.Name
}
