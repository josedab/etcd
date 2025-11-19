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
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboardAssetsEmbedded(t *testing.T) {
	// Test that the dashboard assets are properly embedded
	fsys, err := fs.Sub(dashboardAssets, "dashboard")
	require.NoError(t, err, "should create sub filesystem")

	// Check that index.html exists
	_, err = fs.Stat(fsys, "index.html")
	require.NoError(t, err, "index.html should exist")

	// Check that style.css exists
	_, err = fs.Stat(fsys, "style.css")
	require.NoError(t, err, "style.css should exist")

	// Check that dashboard.js exists
	_, err = fs.Stat(fsys, "dashboard.js")
	require.NoError(t, err, "dashboard.js should exist")
}

func TestDashboardStatusResponseJSON(t *testing.T) {
	resp := DashboardStatusResponse{
		ClusterID:      "abc123",
		ClusterHealth:  "healthy",
		LeaderID:       "member1",
		MemberID:       "member2",
		MembersTotal:   3,
		MembersHealthy: 3,
		Version:        "3.7.0",
		DBSize:         1234567890,
		DBSizeInUse:    800000000,
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err, "should marshal to JSON")

	var decoded DashboardStatusResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "should unmarshal from JSON")

	assert.Equal(t, resp.ClusterID, decoded.ClusterID)
	assert.Equal(t, resp.ClusterHealth, decoded.ClusterHealth)
	assert.Equal(t, resp.LeaderID, decoded.LeaderID)
	assert.Equal(t, resp.MemberID, decoded.MemberID)
	assert.Equal(t, resp.MembersTotal, decoded.MembersTotal)
	assert.Equal(t, resp.MembersHealthy, decoded.MembersHealthy)
	assert.Equal(t, resp.Version, decoded.Version)
	assert.Equal(t, resp.DBSize, decoded.DBSize)
	assert.Equal(t, resp.DBSizeInUse, decoded.DBSizeInUse)
}

func TestDashboardMemberResponseJSON(t *testing.T) {
	resp := DashboardMemberResponse{
		ID:         "member1",
		Name:       "test-member",
		PeerURLs:   []string{"http://localhost:2380"},
		ClientURLs: []string{"http://localhost:2379"},
		IsLeader:   true,
		IsLearner:  false,
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err, "should marshal to JSON")

	var decoded DashboardMemberResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "should unmarshal from JSON")

	assert.Equal(t, resp.ID, decoded.ID)
	assert.Equal(t, resp.Name, decoded.Name)
	assert.Equal(t, resp.PeerURLs, decoded.PeerURLs)
	assert.Equal(t, resp.ClientURLs, decoded.ClientURLs)
	assert.Equal(t, resp.IsLeader, decoded.IsLeader)
	assert.Equal(t, resp.IsLearner, decoded.IsLearner)
}

func TestDashboardMembersResponseJSON(t *testing.T) {
	resp := DashboardMembersResponse{
		Members: []DashboardMemberResponse{
			{
				ID:         "member1",
				Name:       "test-member-1",
				PeerURLs:   []string{"http://localhost:2380"},
				ClientURLs: []string{"http://localhost:2379"},
				IsLeader:   true,
				IsLearner:  false,
			},
			{
				ID:         "member2",
				Name:       "test-member-2",
				PeerURLs:   []string{"http://localhost:2381"},
				ClientURLs: []string{"http://localhost:2378"},
				IsLeader:   false,
				IsLearner:  false,
			},
		},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err, "should marshal to JSON")

	var decoded DashboardMembersResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "should unmarshal from JSON")

	require.Len(t, decoded.Members, 2)
	assert.Equal(t, resp.Members[0].ID, decoded.Members[0].ID)
	assert.Equal(t, resp.Members[1].ID, decoded.Members[1].ID)
}

func TestDashboardMetricsResponseJSON(t *testing.T) {
	resp := DashboardMetricsResponse{
		ProposalsCommitted: 1234567,
		ProposalsFailed:    3,
		LeaderChanges:      2,
		DBSize:             1234567890,
		DBSizeInUse:        800000000,
		KeysTotal:          45678,
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err, "should marshal to JSON")

	var decoded DashboardMetricsResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "should unmarshal from JSON")

	assert.Equal(t, resp.ProposalsCommitted, decoded.ProposalsCommitted)
	assert.Equal(t, resp.ProposalsFailed, decoded.ProposalsFailed)
	assert.Equal(t, resp.LeaderChanges, decoded.LeaderChanges)
	assert.Equal(t, resp.DBSize, decoded.DBSize)
	assert.Equal(t, resp.DBSizeInUse, decoded.DBSizeInUse)
	assert.Equal(t, resp.KeysTotal, decoded.KeysTotal)
}

func TestDashboardHandlerMethodNotAllowed(t *testing.T) {
	// Create a minimal handler that doesn't need a server
	handler := &DashboardHandler{
		server: nil, // Will panic if actually used
		logger: nil,
	}

	tests := []struct {
		name    string
		path    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"status", "/dashboard/api/status", handler.handleStatus},
		{"members", "/dashboard/api/members", handler.handleMembers},
		{"metrics", "/dashboard/api/metrics", handler.handleMetrics},
	}

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}

	for _, tt := range tests {
		for _, method := range methods {
			t.Run(tt.name+"_"+method, func(t *testing.T) {
				req := httptest.NewRequest(method, tt.path, nil)
				w := httptest.NewRecorder()

				tt.handler(w, req)

				assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
			})
		}
	}
}

func TestDashboardConfigDefaults(t *testing.T) {
	cfg := NewConfig()

	// Dashboard should be disabled by default
	assert.False(t, cfg.EnableDashboard)

	// Dashboard address should have a default value
	assert.Equal(t, DefaultDashboardAddr, cfg.DashboardAddr)
}

func TestStaticAssetsContent(t *testing.T) {
	fsys, err := fs.Sub(dashboardAssets, "dashboard")
	require.NoError(t, err)

	// Read and verify index.html contains expected content
	indexContent, err := fs.ReadFile(fsys, "index.html")
	require.NoError(t, err)
	assert.Contains(t, string(indexContent), "etcd Dashboard")
	assert.Contains(t, string(indexContent), "cluster-health")

	// Read and verify style.css contains expected content
	styleContent, err := fs.ReadFile(fsys, "style.css")
	require.NoError(t, err)
	assert.Contains(t, string(styleContent), "etcd Dashboard")

	// Read and verify dashboard.js contains expected content
	jsContent, err := fs.ReadFile(fsys, "dashboard.js")
	require.NoError(t, err)
	assert.Contains(t, string(jsContent), "fetchStatus")
	assert.Contains(t, string(jsContent), "fetchMembers")
	assert.Contains(t, string(jsContent), "fetchMetrics")
}
