// etcd Dashboard JavaScript

// Format bytes to human readable format
function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

// Format number with commas
function formatNumber(num) {
    return num.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ',');
}

// Update the health badge
function updateHealthBadge(health) {
    const badge = document.getElementById('cluster-health');
    badge.textContent = health === 'healthy' ? 'HEALTHY' : 'UNHEALTHY';
    badge.className = 'health-badge ' + health;
}

// Fetch and display cluster status
async function fetchStatus() {
    try {
        const resp = await fetch('/dashboard/api/status');
        if (!resp.ok) {
            throw new Error('Failed to fetch status');
        }
        const data = await resp.json();

        updateHealthBadge(data.cluster_health);
        document.getElementById('cluster-id').textContent = data.cluster_id;
        document.getElementById('member-id').textContent = data.member_id;
        document.getElementById('leader-id').textContent = data.leader_id;
        document.getElementById('version').textContent = data.version;
        document.getElementById('members-count').textContent =
            data.members_healthy + '/' + data.members_total + ' healthy';
        document.getElementById('db-size').textContent = formatBytes(data.db_size);
        document.getElementById('db-size-in-use').textContent = formatBytes(data.db_size_in_use);
    } catch (err) {
        console.error('Error fetching status:', err);
        updateHealthBadge('unhealthy');
    }
}

// Fetch and display cluster members
async function fetchMembers() {
    try {
        const resp = await fetch('/dashboard/api/members');
        if (!resp.ok) {
            throw new Error('Failed to fetch members');
        }
        const data = await resp.json();

        const tbody = document.getElementById('members-tbody');
        tbody.innerHTML = '';

        if (data.members.length === 0) {
            tbody.innerHTML = '<tr><td colspan="5">No members found</td></tr>';
            return;
        }

        data.members.forEach(member => {
            const row = document.createElement('tr');

            // Determine role
            let role = 'Follower';
            let roleClass = 'follower';
            if (member.is_leader) {
                role = 'Leader';
                roleClass = 'leader';
            } else if (member.is_learner) {
                role = 'Learner';
                roleClass = 'learner';
            }

            row.innerHTML = `
                <td>${member.name || '-'}</td>
                <td>${member.id}</td>
                <td><span class="role-badge ${roleClass}">${role}</span></td>
                <td>${member.peer_urls ? member.peer_urls.join(', ') : '-'}</td>
                <td>${member.client_urls ? member.client_urls.join(', ') : '-'}</td>
            `;
            tbody.appendChild(row);
        });
    } catch (err) {
        console.error('Error fetching members:', err);
        const tbody = document.getElementById('members-tbody');
        tbody.innerHTML = '<tr><td colspan="5">Error loading members</td></tr>';
    }
}

// Fetch and display metrics
async function fetchMetrics() {
    try {
        const resp = await fetch('/dashboard/api/metrics');
        if (!resp.ok) {
            throw new Error('Failed to fetch metrics');
        }
        const data = await resp.json();

        document.getElementById('proposals-committed').textContent =
            formatNumber(data.proposals_committed);
        document.getElementById('proposals-failed').textContent =
            formatNumber(data.proposals_failed);
        document.getElementById('leader-changes').textContent =
            formatNumber(data.leader_changes);
        document.getElementById('keys-total').textContent =
            formatNumber(data.keys_total);
    } catch (err) {
        console.error('Error fetching metrics:', err);
    }
}

// Update last updated timestamp
function updateTimestamp() {
    const now = new Date();
    document.getElementById('last-updated').textContent = now.toLocaleString();
}

// Fetch all data
async function fetchAll() {
    await Promise.all([
        fetchStatus(),
        fetchMembers(),
        fetchMetrics()
    ]);
    updateTimestamp();
}

// Initial fetch
fetchAll();

// Refresh every 5 seconds
setInterval(fetchAll, 5000);
