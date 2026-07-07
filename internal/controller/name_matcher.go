package controller

import (
	"fmt"
	"strings"
)

type NameMatcher struct {
	nodeToServerTemplate string
	serverToNodeTemplate string
}

func NewNameMatcher(nodeToServerTemplate string, serverToNodeTemplate string) (NameMatcher, error) {
	nodeToServerTemplate = strings.TrimSpace(nodeToServerTemplate)
	serverToNodeTemplate = strings.TrimSpace(serverToNodeTemplate)
	if nodeToServerTemplate != "" && serverToNodeTemplate != "" {
		return NameMatcher{}, fmt.Errorf("only one matching template can be configured")
	}
	if err := validateNameTemplate(nodeToServerTemplate); err != nil {
		return NameMatcher{}, fmt.Errorf("invalid node-to-server template: %w", err)
	}
	if err := validateNameTemplate(serverToNodeTemplate); err != nil {
		return NameMatcher{}, fmt.Errorf("invalid server-to-node template: %w", err)
	}
	return NameMatcher{nodeToServerTemplate: nodeToServerTemplate, serverToNodeTemplate: serverToNodeTemplate}, nil
}

func DefaultNameMatcher() NameMatcher {
	return NameMatcher{}
}

func (m NameMatcher) Match(nodeName string, serverName string) bool {
	if m.nodeToServerTemplate != "" {
		return fmt.Sprintf(m.nodeToServerTemplate, nodeName) == serverName
	}
	if m.serverToNodeTemplate != "" {
		return fmt.Sprintf(m.serverToNodeTemplate, serverName) == nodeName
	}
	return nodeName == serverName
}

func (m NameMatcher) FindServerForNode(nodeName string, store *ServerStateStore) (KamateraServer, bool) {
	if store == nil {
		return KamateraServer{}, false
	}
	if m.nodeToServerTemplate != "" {
		return store.Get(fmt.Sprintf(m.nodeToServerTemplate, nodeName))
	}
	var matched KamateraServer
	matches := 0
	for _, server := range store.List() {
		if m.Match(nodeName, server.Name) {
			matched = server
			matches++
		}
	}
	return matched, matches == 1
}

func (m NameMatcher) FindNodeForServer(serverName string, store *NodeStateStore) (NodeSnapshot, bool) {
	if store == nil {
		return NodeSnapshot{}, false
	}
	if m.serverToNodeTemplate != "" {
		return store.Get(fmt.Sprintf(m.serverToNodeTemplate, serverName))
	}
	for _, node := range store.List() {
		if m.Match(node.Name, serverName) {
			return node, true
		}
	}
	return NodeSnapshot{}, false
}

func validateNameTemplate(template string) error {
	if template == "" {
		return nil
	}
	if strings.Count(template, "%s") != 1 {
		return fmt.Errorf("template must contain exactly one %%s placeholder")
	}
	if strings.Contains(strings.ReplaceAll(template, "%s", ""), "%") {
		return fmt.Errorf("template must not contain format verbs other than %%s")
	}
	return nil
}
