package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/query"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/gorilla/mux"
)

type Server struct {
	graph     *storage.GraphStorage
	traverser *query.Traverser
	startTime time.Time
}

func main() {
	dataDir := flag.String("data", "./data/graphdb", "Data directory")
	port := flag.Int("port", 8080, "Server port")
	flag.Parse()

	log.Printf("🚀 Cluso GraphDB Server starting...")
	log.Printf("📂 Data directory: %s", *dataDir)

	// Initialize graph
	graph, err := storage.NewGraphStorage(*dataDir)
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	server := &Server{
		graph:     graph,
		traverser: query.NewTraverser(graph),
		startTime: time.Now(),
	}

	router := mux.NewRouter()

	// Node endpoints
	router.HandleFunc("/nodes", server.createNode).Methods("POST")
	router.HandleFunc("/nodes/{id}", server.getNode).Methods("GET")
	router.HandleFunc("/nodes/{id}", server.updateNode).Methods("PUT")
	router.HandleFunc("/nodes/{id}", server.deleteNode).Methods("DELETE")
	router.HandleFunc("/nodes", server.findNodes).Methods("GET")

	// Edge endpoints
	router.HandleFunc("/edges", server.createEdge).Methods("POST")
	router.HandleFunc("/edges/{id}", server.getEdge).Methods("GET")
	router.HandleFunc("/nodes/{id}/edges/outgoing", server.getOutgoingEdges).Methods("GET")
	router.HandleFunc("/nodes/{id}/edges/incoming", server.getIncomingEdges).Methods("GET")

	// Query endpoints
	router.HandleFunc("/traverse", server.traverse).Methods("POST")
	router.HandleFunc("/path/shortest", server.findShortestPath).Methods("POST")
	router.HandleFunc("/path/all", server.findAllPaths).Methods("POST")

	// Admin endpoints
	router.HandleFunc("/health", server.health).Methods("GET")
	router.HandleFunc("/stats", server.stats).Methods("GET")
	router.HandleFunc("/snapshot", server.snapshot).Methods("POST")

	// CORS middleware
	router.Use(corsMiddleware)

	// Logging middleware
	router.Use(loggingMiddleware)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("✅ Server listening on %s", addr)
	log.Printf("📊 Health check: http://localhost%s/health", addr)

	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// Middleware

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.RequestURI, time.Since(start))
	})
}

// Node handlers

func (s *Server) createNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Labels     []string               `json:"labels"`
		Properties map[string]interface{} `json:"properties"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Convert properties
	props := make(map[string]storage.Value)
	for k, v := range req.Properties {
		props[k] = convertToValue(v)
	}

	node, err := s.graph.CreateNode(req.Labels, props)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         node.ID,
		"labels":     node.Labels,
		"created_at": node.CreatedAt,
	})
}

func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseUint(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid node ID", http.StatusBadRequest)
		return
	}

	node, err := s.graph.GetNode(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodeToJSON(node))
}

func (s *Server) updateNode(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseUint(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid node ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Properties map[string]interface{} `json:"properties"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	props := make(map[string]storage.Value)
	for k, v := range req.Properties {
		props[k] = convertToValue(v)
	}

	if err := s.graph.UpdateNode(id, props); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseUint(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid node ID", http.StatusBadRequest)
		return
	}

	if err := s.graph.DeleteNode(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func (s *Server) findNodes(w http.ResponseWriter, r *http.Request) {
	label := r.URL.Query().Get("label")

	if label == "" {
		http.Error(w, "label parameter required", http.StatusBadRequest)
		return
	}

	nodes, err := s.graph.FindNodesByLabel(label)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result := make([]interface{}, len(nodes))
	for i, node := range nodes {
		result[i] = nodeToJSON(node)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// Edge handlers

func (s *Server) createEdge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FromNodeID uint64                 `json:"from_node_id"`
		ToNodeID   uint64                 `json:"to_node_id"`
		Type       string                 `json:"type"`
		Properties map[string]interface{} `json:"properties"`
		Weight     float64                `json:"weight"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	props := make(map[string]storage.Value)
	for k, v := range req.Properties {
		props[k] = convertToValue(v)
	}

	edge, err := s.graph.CreateEdge(req.FromNodeID, req.ToNodeID, req.Type, props, req.Weight)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":           edge.ID,
		"from_node_id": edge.FromNodeID,
		"to_node_id":   edge.ToNodeID,
		"type":         edge.Type,
		"created_at":   edge.CreatedAt,
	})
}

func (s *Server) getEdge(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseUint(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid edge ID", http.StatusBadRequest)
		return
	}

	edge, err := s.graph.GetEdge(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(edgeToJSON(edge))
}

func (s *Server) getOutgoingEdges(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseUint(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid node ID", http.StatusBadRequest)
		return
	}

	edges, err := s.graph.GetOutgoingEdges(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result := make([]interface{}, len(edges))
	for i, edge := range edges {
		result[i] = edgeToJSON(edge)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) getIncomingEdges(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseUint(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid node ID", http.StatusBadRequest)
		return
	}

	edges, err := s.graph.GetIncomingEdges(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result := make([]interface{}, len(edges))
	for i, edge := range edges {
		result[i] = edgeToJSON(edge)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// Query handlers

func (s *Server) traverse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StartNodeID uint64   `json:"start_node_id"`
		Direction   string   `json:"direction"`
		EdgeTypes   []string `json:"edge_types"`
		MaxDepth    int      `json:"max_depth"`
		MaxResults  int      `json:"max_results"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	direction := query.DirectionOutgoing
	switch req.Direction {
	case "incoming":
		direction = query.DirectionIncoming
	case "both":
		direction = query.DirectionBoth
	}

	result, err := s.traverser.BFS(query.TraversalOptions{
		StartNodeID: req.StartNodeID,
		Direction:   direction,
		EdgeTypes:   req.EdgeTypes,
		MaxDepth:    req.MaxDepth,
		MaxResults:  req.MaxResults,
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	nodes := make([]interface{}, len(result.Nodes))
	for i, node := range result.Nodes {
		nodes[i] = nodeToJSON(node)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodes": nodes,
		"count": len(nodes),
	})
}

func (s *Server) findShortestPath(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FromNodeID uint64   `json:"from_node_id"`
		ToNodeID   uint64   `json:"to_node_id"`
		EdgeTypes  []string `json:"edge_types"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	path, err := s.traverser.FindShortestPath(req.FromNodeID, req.ToNodeID, req.EdgeTypes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	nodes := make([]interface{}, len(path.Nodes))
	for i, node := range path.Nodes {
		nodes[i] = nodeToJSON(node)
	}

	edges := make([]interface{}, len(path.Edges))
	for i, edge := range path.Edges {
		edges[i] = edgeToJSON(edge)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodes":  nodes,
		"edges":  edges,
		"length": len(path.Edges),
	})
}

func (s *Server) findAllPaths(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FromNodeID uint64   `json:"from_node_id"`
		ToNodeID   uint64   `json:"to_node_id"`
		MaxDepth   int      `json:"max_depth"`
		EdgeTypes  []string `json:"edge_types"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	paths, err := s.traverser.FindAllPaths(req.FromNodeID, req.ToNodeID, req.MaxDepth, req.EdgeTypes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result := make([]interface{}, len(paths))
	for i, path := range paths {
		nodes := make([]interface{}, len(path.Nodes))
		for j, node := range path.Nodes {
			nodes[j] = nodeToJSON(node)
		}

		edges := make([]interface{}, len(path.Edges))
		for j, edge := range path.Edges {
			edges[j] = edgeToJSON(edge)
		}

		result[i] = map[string]interface{}{
			"nodes":  nodes,
			"edges":  edges,
			"length": len(path.Edges),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"paths": result,
		"count": len(paths),
	})
}

// Admin handlers

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	stats := s.graph.GetStatistics()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "healthy",
		"uptime":     time.Since(s.startTime).String(),
		"node_count": stats.NodeCount,
		"edge_count": stats.EdgeCount,
	})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	stats := s.graph.GetStatistics()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (s *Server) snapshot(w http.ResponseWriter, r *http.Request) {
	if err := s.graph.Snapshot(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Snapshot created",
	})
}

// Helper functions

func convertToValue(v interface{}) storage.Value {
	switch val := v.(type) {
	case string:
		return storage.StringValue(val)
	case float64:
		return storage.IntValue(int64(val))
	case bool:
		return storage.BoolValue(val)
	default:
		return storage.StringValue(fmt.Sprint(v))
	}
}

func nodeToJSON(node *storage.Node) map[string]interface{} {
	props := make(map[string]interface{})
	for k, v := range node.Properties {
		props[k] = valueToInterface(v)
	}

	return map[string]interface{}{
		"id":         node.ID,
		"labels":     node.Labels,
		"properties": props,
		"created_at": node.CreatedAt,
		"updated_at": node.UpdatedAt,
	}
}

func edgeToJSON(edge *storage.Edge) map[string]interface{} {
	props := make(map[string]interface{})
	for k, v := range edge.Properties {
		props[k] = valueToInterface(v)
	}

	return map[string]interface{}{
		"id":           edge.ID,
		"from_node_id": edge.FromNodeID,
		"to_node_id":   edge.ToNodeID,
		"type":         edge.Type,
		"properties":   props,
		"weight":       edge.Weight,
		"created_at":   edge.CreatedAt,
	}
}

func valueToInterface(v storage.Value) interface{} {
	switch v.Type {
	case storage.TypeString:
		s, _ := v.AsString()
		return s
	case storage.TypeInt:
		i, _ := v.AsInt()
		return i
	case storage.TypeFloat:
		f, _ := v.AsFloat()
		return f
	case storage.TypeBool:
		b, _ := v.AsBool()
		return b
	case storage.TypeTimestamp:
		t, _ := v.AsTimestamp()
		return t.Unix()
	default:
		return nil
	}
}
