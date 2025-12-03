<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api/client';
	import { Icon } from '$lib/components';
	import type { HealthResponse, SecurityHealth, MetricsResponse } from '$lib/api/types';

	let health = $state<HealthResponse | null>(null);
	let securityHealth = $state<SecurityHealth | null>(null);
	let metrics = $state<MetricsResponse | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let lastUpdated = $state<Date | null>(null);
	let autoRefresh = $state(true);
	let refreshInterval: ReturnType<typeof setInterval> | null = null;

	async function loadData() {
		try {
			const [h, sh, m] = await Promise.all([
				api.getHealth(),
				api.getSecurityHealth().catch(() => null),
				api.getMetrics().catch(() => null)
			]);
			health = h;
			securityHealth = sh;
			metrics = m;
			lastUpdated = new Date();
			error = null;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load dashboard';
		} finally {
			loading = false;
		}
	}

	function startAutoRefresh() {
		if (refreshInterval) clearInterval(refreshInterval);
		if (autoRefresh) {
			refreshInterval = setInterval(loadData, 30000); // 30 seconds
		}
	}

	function toggleAutoRefresh() {
		autoRefresh = !autoRefresh;
		startAutoRefresh();
	}

	onMount(() => {
		loadData();
		startAutoRefresh();
	});

	onDestroy(() => {
		if (refreshInterval) clearInterval(refreshInterval);
	});

	function getStatusColor(status: string): string {
		switch (status?.toLowerCase()) {
			case 'healthy':
			case 'ok':
				return 'success';
			case 'degraded':
				return 'warning';
			default:
				return 'error';
		}
	}

	function formatNumber(num: number): string {
		if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
		if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
		return num.toString();
	}

	function formatBytes(bytes: number): string {
		if (bytes >= 1073741824) return (bytes / 1073741824).toFixed(1) + ' GB';
		if (bytes >= 1048576) return (bytes / 1048576).toFixed(1) + ' MB';
		if (bytes >= 1024) return (bytes / 1024).toFixed(1) + ' KB';
		return bytes + ' B';
	}

	function formatTime(date: Date): string {
		return date.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
	}
</script>

<div class="max-w-6xl">
	<header class="flex justify-between items-start mb-6 flex-wrap gap-4">
		<div>
			<h1 class="text-2xl font-bold mb-1">Dashboard</h1>
			<p class="text-[--color-text-secondary]">System overview and health status</p>
		</div>
		<div class="flex items-center gap-3">
			{#if lastUpdated}
				<span class="text-xs text-[--color-text-muted]">
					Updated {formatTime(lastUpdated)}
				</span>
			{/if}
			<button
				class="btn btn-ghost btn-icon"
				title={autoRefresh ? 'Disable auto-refresh' : 'Enable auto-refresh'}
				onclick={toggleAutoRefresh}
			>
				<Icon name={autoRefresh ? 'pause' : 'play'} size={16} />
			</button>
			<button
				class="btn btn-secondary flex items-center gap-2"
				onclick={loadData}
				disabled={loading}
			>
				<Icon name="arrow-path" size={16} class={loading ? 'animate-spin' : ''} />
				Refresh
			</button>
		</div>
	</header>

	{#if loading}
		<div class="flex flex-col items-center justify-center py-16 gap-4 text-[--color-text-secondary]">
			<div class="spinner"></div>
			<p>Loading dashboard...</p>
		</div>
	{:else if error}
		<div class="alert alert-error">{error}</div>
	{:else}
		<!-- Primary Stats Grid -->
		<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
			<div class="card flex items-center gap-4">
				<div class="w-12 h-12 bg-[--color-bg-tertiary] rounded-lg flex items-center justify-center">
					<span class="status-dot {getStatusColor(health?.status || '')}"></span>
				</div>
				<div>
					<div class="text-xs text-[--color-text-muted] uppercase tracking-wider mb-1">System Status</div>
					<div class="text-lg font-semibold capitalize">{health?.status || 'Unknown'}</div>
				</div>
			</div>

			<div class="card flex items-center gap-4">
				<div class="w-12 h-12 bg-[--color-bg-tertiary] rounded-lg flex items-center justify-center">
					<Icon name="circle-stack" size={24} class="text-[--color-primary]" />
				</div>
				<div>
					<div class="text-xs text-[--color-text-muted] uppercase tracking-wider mb-1">Nodes</div>
					<div class="text-lg font-semibold font-mono">{metrics ? formatNumber(metrics.node_count) : '—'}</div>
				</div>
			</div>

			<div class="card flex items-center gap-4">
				<div class="w-12 h-12 bg-[--color-bg-tertiary] rounded-lg flex items-center justify-center">
					<Icon name="arrow-right" size={24} class="text-[--color-success]" />
				</div>
				<div>
					<div class="text-xs text-[--color-text-muted] uppercase tracking-wider mb-1">Edges</div>
					<div class="text-lg font-semibold font-mono">{metrics ? formatNumber(metrics.edge_count) : '—'}</div>
				</div>
			</div>

			<div class="card flex items-center gap-4">
				<div class="w-12 h-12 bg-[--color-bg-tertiary] rounded-lg flex items-center justify-center">
					<Icon name="clock" size={24} class="text-[--color-warning]" />
				</div>
				<div>
					<div class="text-xs text-[--color-text-muted] uppercase tracking-wider mb-1">Uptime</div>
					<div class="text-lg font-semibold">{health?.uptime || 'N/A'}</div>
				</div>
			</div>
		</div>

		<!-- Database Metrics -->
		{#if metrics}
			<div class="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-6 gap-3 mb-6">
				<div class="card p-4">
					<div class="text-xs text-[--color-text-muted] uppercase tracking-wider mb-1">Total Queries</div>
					<div class="text-xl font-bold font-mono">{formatNumber(metrics.total_queries)}</div>
				</div>
				<div class="card p-4">
					<div class="text-xs text-[--color-text-muted] uppercase tracking-wider mb-1">Avg Latency</div>
					<div class="text-xl font-bold font-mono">{metrics.avg_query_time_ms.toFixed(1)}ms</div>
				</div>
				<div class="card p-4">
					<div class="text-xs text-[--color-text-muted] uppercase tracking-wider mb-1">Memory Used</div>
					<div class="text-xl font-bold font-mono">{metrics.memory_used_mb}MB</div>
					<div class="text-xs text-[--color-text-muted]">of {metrics.memory_total_mb}MB</div>
				</div>
				<div class="card p-4">
					<div class="text-xs text-[--color-text-muted] uppercase tracking-wider mb-1">Goroutines</div>
					<div class="text-xl font-bold font-mono">{metrics.num_goroutines}</div>
				</div>
				<div class="card p-4">
					<div class="text-xs text-[--color-text-muted] uppercase tracking-wider mb-1">CPU Cores</div>
					<div class="text-xl font-bold font-mono">{metrics.num_cpu}</div>
				</div>
				<div class="card p-4">
					<div class="text-xs text-[--color-text-muted] uppercase tracking-wider mb-1">Edition</div>
					<div class="text-xl font-bold">{health?.edition || 'Community'}</div>
				</div>
			</div>
		{/if}

		<!-- Panels Grid -->
		<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
			<!-- Features -->
			<section class="card">
				<h2 class="text-sm font-semibold text-[--color-text-secondary] mb-4">Features</h2>
				<div class="space-y-2">
					{#if health?.features && health.features.length > 0}
						{#each health.features as feature}
							<div class="flex items-center gap-2">
								<Icon name="check-circle" size={16} class="text-[--color-success]" />
								<span>{feature}</span>
							</div>
						{/each}
					{:else}
						<p class="text-[--color-text-muted] text-sm">No features reported</p>
					{/if}
				</div>
			</section>

			<!-- Security Status -->
			<section class="card">
				<h2 class="text-sm font-semibold text-[--color-text-secondary] mb-4">Security Status</h2>
				{#if securityHealth}
					<div class="grid grid-cols-2 gap-3">
						<div class="flex justify-between items-center py-2">
							<span class="text-sm text-[--color-text-secondary]">Encryption</span>
							<span class="badge badge-{securityHealth.components.encryption.enabled ? 'success' : 'warning'}">
								{securityHealth.components.encryption.enabled ? 'Enabled' : 'Disabled'}
							</span>
						</div>
						<div class="flex justify-between items-center py-2">
							<span class="text-sm text-[--color-text-secondary]">TLS</span>
							<span class="badge badge-{securityHealth.components.tls.enabled ? 'success' : 'warning'}">
								{securityHealth.components.tls.enabled ? 'Enabled' : 'Disabled'}
							</span>
						</div>
						<div class="flex justify-between items-center py-2">
							<span class="text-sm text-[--color-text-secondary]">Audit Logging</span>
							<span class="badge badge-{securityHealth.components.audit.enabled ? 'success' : 'warning'}">
								{securityHealth.components.audit.enabled ? 'Enabled' : 'Disabled'}
							</span>
						</div>
						<div class="flex justify-between items-center py-2">
							<span class="text-sm text-[--color-text-secondary]">JWT Auth</span>
							<span class="badge badge-{securityHealth.components.authentication.jwt_enabled ? 'success' : 'warning'}">
								{securityHealth.components.authentication.jwt_enabled ? 'Enabled' : 'Disabled'}
							</span>
						</div>
						<div class="flex justify-between items-center py-2">
							<span class="text-sm text-[--color-text-secondary]">API Key Auth</span>
							<span class="badge badge-{securityHealth.components.authentication.apikey_enabled ? 'success' : 'warning'}">
								{securityHealth.components.authentication.apikey_enabled ? 'Enabled' : 'Disabled'}
							</span>
						</div>
						{#if securityHealth.components.audit.event_count !== undefined}
							<div class="flex justify-between items-center py-2">
								<span class="text-sm text-[--color-text-secondary]">Audit Events</span>
								<span class="font-semibold font-mono">{securityHealth.components.audit.event_count.toLocaleString()}</span>
							</div>
						{/if}
					</div>
				{:else}
					<p class="text-[--color-text-muted] text-sm">Security health information unavailable</p>
				{/if}
			</section>

			<!-- Health Checks -->
			<section class="card">
				<h2 class="text-sm font-semibold text-[--color-text-secondary] mb-4">Health Checks</h2>
				{#if health?.checks && Object.keys(health.checks).length > 0}
					<div class="space-y-3">
						{#each Object.entries(health.checks) as [name, check]}
							<div class="flex justify-between items-center">
								<span class="text-sm capitalize">{name}</span>
								<span class="badge badge-{getStatusColor(String(check))}">
									{String(check)}
								</span>
							</div>
						{/each}
					</div>
				{:else}
					<p class="text-[--color-text-muted] text-sm">No health checks available</p>
				{/if}
			</section>

			<!-- Quick Actions -->
			<section class="card">
				<h2 class="text-sm font-semibold text-[--color-text-secondary] mb-4">Quick Actions</h2>
				<div class="grid grid-cols-2 gap-3">
					<a href="/explorer" class="flex flex-col items-center gap-2 p-4 bg-[--color-bg-tertiary] rounded-lg hover:bg-[--color-border] transition-colors text-center group">
						<Icon name="magnifying-glass" size={28} class="text-[--color-primary] group-hover:scale-110 transition-transform" />
						<span>Query Database</span>
					</a>
					<a href="/apikeys" class="flex flex-col items-center gap-2 p-4 bg-[--color-bg-tertiary] rounded-lg hover:bg-[--color-border] transition-colors text-center group">
						<Icon name="key" size={28} class="text-[--color-warning] group-hover:scale-110 transition-transform" />
						<span>Manage API Keys</span>
					</a>
					<a href="/metrics" class="flex flex-col items-center gap-2 p-4 bg-[--color-bg-tertiary] rounded-lg hover:bg-[--color-border] transition-colors text-center group">
						<Icon name="chart-bar" size={28} class="text-[--color-success] group-hover:scale-110 transition-transform" />
						<span>View Metrics</span>
					</a>
					<a href="/audit" class="flex flex-col items-center gap-2 p-4 bg-[--color-bg-tertiary] rounded-lg hover:bg-[--color-border] transition-colors text-center group">
						<Icon name="document-text" size={28} class="text-[--color-text-secondary] group-hover:scale-110 transition-transform" />
						<span>Audit Logs</span>
					</a>
				</div>
			</section>
		</div>
	{/if}
</div>
