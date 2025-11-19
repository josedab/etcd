// Copyright 2025 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package embed

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"

	"go.uber.org/zap"

	"go.etcd.io/etcd/api/v3/version"
	"go.etcd.io/etcd/server/v3/etcdserver"
	"go.etcd.io/raft/v3"
)

//go:embed dashboard/*
var dashboardAssets embed.FS

// DashboardStatusResponse represents the cluster status response.
type DashboardStatusResponse struct {
	ClusterID      string `json:"cluster_id"`
	ClusterHealth  string `json:"cluster_health"`
	LeaderID       string `json:"leader_id"`
	MemberID       string `json:"member_id"`
	MembersTotal   int    `json:"members_total"`
	MembersHealthy int    `json:"members_healthy"`
	Version        string `json:"version"`
	DBSize         int64  `json:"db_size"`
	DBSizeInUse    int64  `json:"db_size_in_use"`
}

// DashboardMemberResponse represents a single member in the cluster.
type DashboardMemberResponse struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	PeerURLs   []string `json:"peer_urls"`
	ClientURLs []string `json:"client_urls"`
	IsLeader   bool     `json:"is_leader"`
	IsLearner  bool     `json:"is_learner"`
}

// DashboardMembersResponse represents the members list response.
type DashboardMembersResponse struct {
	Members []DashboardMemberResponse `json:"members"`
}

// DashboardMetricsResponse represents the key metrics response.
type DashboardMetricsResponse struct {
	ProposalsCommitted uint64 `json:"proposals_committed"`
	ProposalsFailed    uint64 `json:"proposals_failed"`
	LeaderChanges      uint64 `json:"leader_changes"`
	DBSize             int64  `json:"db_size"`
	DBSizeInUse        int64  `json:"db_size_in_use"`
	KeysTotal          int64  `json:"keys_total"`
}

// DashboardHandler handles dashboard HTTP requests.
type DashboardHandler struct {
	server *etcdserver.EtcdServer
	logger *zap.Logger
}

// NewDashboardHandler creates a new dashboard handler.
func NewDashboardHandler(server *etcdserver.EtcdServer, logger *zap.Logger) *DashboardHandler {
	return &DashboardHandler{
		server: server,
		logger: logger,
	}
}

// RegisterDashboardHandlers registers all dashboard HTTP handlers.
func (dh *DashboardHandler) RegisterDashboardHandlers(mux *http.ServeMux) {
	// API endpoints
	mux.HandleFunc("/dashboard/api/status", dh.handleStatus)
	mux.HandleFunc("/dashboard/api/members", dh.handleMembers)
	mux.HandleFunc("/dashboard/api/metrics", dh.handleMetrics)

	// Static assets
	fsys, err := fs.Sub(dashboardAssets, "dashboard")
	if err != nil {
		dh.logger.Error("failed to create sub filesystem for dashboard assets", zap.Error(err))
		return
	}
	mux.Handle("/dashboard/", http.StripPrefix("/dashboard/", http.FileServer(http.FS(fsys))))
}

// handleStatus returns the cluster status.
func (dh *DashboardHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cluster := dh.server.Cluster()
	members := cluster.Members()

	// Determine cluster health
	clusterHealth := "healthy"
	membersHealthy := 0
	leaderID := dh.server.Leader()

	for _, m := range members {
		if m.IsStarted() {
			membersHealthy++
		}
	}

	if uint64(leaderID) == raft.None {
		clusterHealth = "unhealthy"
	}

	// Get backend info
	be := dh.server.Backend()
	dbSize := be.Size()
	dbSizeInUse := be.SizeInUse()

	resp := DashboardStatusResponse{
		ClusterID:      cluster.ID().String(),
		ClusterHealth:  clusterHealth,
		LeaderID:       leaderID.String(),
		MemberID:       dh.server.MemberID().String(),
		MembersTotal:   len(members),
		MembersHealthy: membersHealthy,
		Version:        version.Version,
		DBSize:         dbSize,
		DBSizeInUse:    dbSizeInUse,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		dh.logger.Error("failed to encode status response", zap.Error(err))
	}
}

// handleMembers returns the cluster members.
func (dh *DashboardHandler) handleMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cluster := dh.server.Cluster()
	members := cluster.Members()
	leaderID := dh.server.Leader()

	var memberResponses []DashboardMemberResponse
	for _, m := range members {
		memberResponses = append(memberResponses, DashboardMemberResponse{
			ID:         m.ID.String(),
			Name:       m.Name,
			PeerURLs:   m.PeerURLs,
			ClientURLs: m.ClientURLs,
			IsLeader:   m.ID == leaderID,
			IsLearner:  m.IsLearner,
		})
	}

	resp := DashboardMembersResponse{
		Members: memberResponses,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		dh.logger.Error("failed to encode members response", zap.Error(err))
	}
}

// handleMetrics returns key metrics.
func (dh *DashboardHandler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get backend info
	be := dh.server.Backend()
	dbSize := be.Size()
	dbSizeInUse := be.SizeInUse()

	// Note: Detailed proposal statistics would require accessing Prometheus metrics.
	// For now, we provide basic storage metrics. Future enhancement could read
	// from the metrics registry directly.
	resp := DashboardMetricsResponse{
		ProposalsCommitted: 0, // Would need Prometheus metrics integration
		ProposalsFailed:    0, // Would need Prometheus metrics integration
		LeaderChanges:      0, // Would need Prometheus metrics integration
		DBSize:             dbSize,
		DBSizeInUse:        dbSizeInUse,
		KeysTotal:          0, // Would need to query the key-value store
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		dh.logger.Error("failed to encode metrics response", zap.Error(err))
	}
}

// serveDashboard starts the dashboard HTTP server.
func (e *Etcd) serveDashboard() error {
	if !e.cfg.EnableDashboard {
		return nil
	}

	e.cfg.logger.Info(
		"starting dashboard server",
		zap.String("address", e.cfg.DashboardAddr),
	)

	mux := http.NewServeMux()
	handler := NewDashboardHandler(e.Server, e.cfg.logger)
	handler.RegisterDashboardHandlers(mux)

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		server := &http.Server{
			Addr:    e.cfg.DashboardAddr,
			Handler: mux,
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- server.ListenAndServe()
		}()

		select {
		case <-e.stopc:
			server.Close()
		case err := <-errCh:
			if err != nil && err != http.ErrServerClosed {
				e.cfg.logger.Error("dashboard server error", zap.Error(err))
				select {
				case e.errc <- err:
				default:
				}
			}
		}
	}()

	return nil
}
