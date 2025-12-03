<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api/client';
	import type { MetricsResponse } from '$lib/api/types';
	import { Icon, Skeleton } from '$lib/components';
	import { toast } from '$lib/stores/toast';

	interface MetricPoint {
		timestamp: number;
		value: number;
	}

	let metrics = $state<MetricsResponse | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let autoRefresh = $state(true);
	let refreshInterval: ReturnType<typeof setInterval>;

	// History for charts (last 60 data points)
	let queryHistory = $state<MetricPoint[]>([]);
	let latencyHistory = $state<MetricPoint[]>([]);
	let memoryHistory = $state<MetricPoint[]>([]);

	onMount(() => {
		loadMetrics();
		if (autoRefresh) {
			startAutoRefresh();
		}
	});

	onDestroy(() => {
		stopAutoRefresh();
	});

	function startAutoRefresh() {
		refreshInterval = setInterval(loadMetrics, 5000);
	}

	function stopAutoRefresh() {
		if (refreshInterval) {
			clearInterval(refreshInterval);
		}
	}

	function toggleAutoRefresh() {
		autoRefresh = !autoRefresh;
		if (autoRefresh) {
			startAutoRefresh();
		} else {
			stopAutoRefresh();
		}
	}

	async function loadMetrics() {
		try {
			const data = await api.getMetrics();
			metrics = data;

			const now = Date.now();

			// Update history (keep last 60 points)
			queryHistory = [...queryHistory, { timestamp: now, value: data.total_queries }].slice(-60);
			latencyHistory = [...latencyHistory, { timestamp: now, value: data.avg_query_time_ms }].slice(-60);
			memoryHistory = [...memoryHistory, { timestamp: now, value: data.memory_used_mb }].slice(-60);

			error = null;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load metrics';
			if (loading) {
				toast.error(error);
			}
		} finally {
			loading = false;
		}
	}

	function formatUptime(seconds: number): string {
		const days = Math.floor(seconds / 86400);
		const hours = Math.floor((seconds % 86400) / 3600);
		const mins = Math.floor((seconds % 3600) / 60);
		if (days > 0) return `${days}d ${hours}h`;
		if (hours > 0) return `${hours}h ${mins}m`;
		return `${mins}m`;
	}

	function getSparkline(data: MetricPoint[], height = 40): string {
		if (data.length < 2) return '';
		const values = data.map(d => d.value);
		const min = Math.min(...values);
		const max = Math.max(...values);
		const range = max - min || 1;
		const width = 200;
		const step = width / (values.length - 1);

		const points = values.map((v, i) => {
			const x = i * step;
			const y = height - ((v - min) / range) * height;
			return `${x},${y}`;
		}).join(' ');

		return points;
	}

	// Calculate queries per second from history
	function getQueriesPerSecond(): number {
		if (queryHistory.length < 2) return 0;
		const first = queryHistory[queryHistory.length - 2];
		const last = queryHistory[queryHistory.length - 1];
		const timeDiff = (last.timestamp - first.timestamp) / 1000;
		const queryDiff = last.value - first.value;
		return timeDiff > 0 ? queryDiff / timeDiff : 0;
	}

	// Derive queries per second from history
	const qps = $derived(getQueriesPerSecond());
</script>

<div class="max-w-6xl">
	<header class="flex justify-between items-start mb-8">
		<div>
			<h1 class="text-2xl font-bold mb-1">Metrics</h1>
			<p class="text-[--color-text-secondary]">Real-time performance monitoring</p>
		</div>
		<div class="flex items-center gap-4">
			<label class="flex items-center gap-2 text-sm cursor-pointer">
				<button
					class="toggle {autoRefresh ? 'active' : ''}"
					onclick={toggleAutoRefresh}
				>
					<span class="toggle-knob"></span>
				</button>
				Auto-refresh
			</label>
			<button class="btn btn-secondary flex items-center gap-2" onclick={loadMetrics}>
				<Icon name="arrow-path" size={16} />
				Refresh
			</button>
		</div>
	</header>

	{#if error && !metrics}
		<div class="card p-6 text-center">
			<Icon name="exclamation-circle" size={48} class="mx-auto mb-4 text-red-500" />
			<p class="text-[--color-text-secondary] mb-4">{error}</p>
			<button class="btn btn-secondary" onclick={loadMetrics}>Try Again</button>
		</div>
	{:else}
		<!-- Key Metrics Grid -->
		<div class="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
			<div class="card">
				<div class="text-sm text-[--color-text-muted] mb-1">Queries/sec</div>
				{#if loading}
					<Skeleton width="4rem" height="2rem" />
				{:else}
					<div class="text-2xl font-bold text-[--color-primary]">{qps.toFixed(1)}</div>
				{/if}
			</div>
			<div class="card">
				<div class="text-sm text-[--color-text-muted] mb-1">Avg Latency</div>
				{#if loading}
					<Skeleton width="4rem" height="2rem" />
				{:else}
					<div class="text-2xl font-bold">{metrics?.avg_query_time_ms.toFixed(1)}ms</div>
				{/if}
			</div>
			<div class="card">
				<div class="text-sm text-[--color-text-muted] mb-1">Goroutines</div>
				{#if loading}
					<Skeleton width="3rem" height="2rem" />
				{:else}
					<div class="text-2xl font-bold">{metrics?.num_goroutines}</div>
				{/if}
			</div>
			<div class="card">
				<div class="text-sm text-[--color-text-muted] mb-1">Uptime</div>
				{#if loading}
					<Skeleton width="4rem" height="2rem" />
				{:else}
					<div class="text-2xl font-bold text-[--color-success]">{formatUptime(metrics?.uptime_seconds || 0)}</div>
				{/if}
			</div>
		</div>

		<!-- Charts -->
		<div class="grid grid-cols-1 lg:grid-cols-3 gap-6 mb-6">
			<!-- Total Queries Chart -->
			<div class="card">
				<h3 class="text-sm font-semibold text-[--color-text-secondary] mb-4">Total Queries</h3>
				<div class="h-24 flex items-end">
					{#if queryHistory.length > 1}
						<svg width="100%" height="100%" viewBox="0 0 200 40" preserveAspectRatio="none">
							<polyline
								points={getSparkline(queryHistory)}
								fill="none"
								stroke="var(--color-primary)"
								stroke-width="2"
							/>
						</svg>
					{:else}
						<div class="w-full h-full flex items-center justify-center text-[--color-text-muted] text-sm">
							Collecting data...
						</div>
					{/if}
				</div>
				<div class="flex justify-between text-xs text-[--color-text-muted] mt-2">
					<span>5 min ago</span>
					<span>{metrics?.total_queries.toLocaleString() || 0} total</span>
				</div>
			</div>

			<!-- Latency Chart -->
			<div class="card">
				<h3 class="text-sm font-semibold text-[--color-text-secondary] mb-4">Query Latency (ms)</h3>
				<div class="h-24 flex items-end">
					{#if latencyHistory.length > 1}
						<svg width="100%" height="100%" viewBox="0 0 200 40" preserveAspectRatio="none">
							<polyline
								points={getSparkline(latencyHistory)}
								fill="none"
								stroke="var(--color-success)"
								stroke-width="2"
							/>
						</svg>
					{:else}
						<div class="w-full h-full flex items-center justify-center text-[--color-text-muted] text-sm">
							Collecting data...
						</div>
					{/if}
				</div>
				<div class="flex justify-between text-xs text-[--color-text-muted] mt-2">
					<span>5 min ago</span>
					<span>Now</span>
				</div>
			</div>

			<!-- Memory Chart -->
			<div class="card">
				<h3 class="text-sm font-semibold text-[--color-text-secondary] mb-4">Memory Usage (MB)</h3>
				<div class="h-24 flex items-end">
					{#if memoryHistory.length > 1}
						<svg width="100%" height="100%" viewBox="0 0 200 40" preserveAspectRatio="none">
							<polyline
								points={getSparkline(memoryHistory)}
								fill="none"
								stroke="var(--color-warning)"
								stroke-width="2"
							/>
						</svg>
					{:else}
						<div class="w-full h-full flex items-center justify-center text-[--color-text-muted] text-sm">
							Collecting data...
						</div>
					{/if}
				</div>
				<div class="flex justify-between text-xs text-[--color-text-muted] mt-2">
					<span>5 min ago</span>
					<span>Now</span>
				</div>
			</div>
		</div>

		<!-- Resource Usage -->
		<div class="grid grid-cols-1 lg:grid-cols-2 gap-6 mb-6">
			<!-- Memory -->
			<div class="card">
				<h3 class="text-sm font-semibold text-[--color-text-secondary] mb-4">Memory</h3>
				{#if loading}
					<Skeleton height="1.5rem" class="mb-2" />
					<Skeleton height="0.75rem" width="40%" />
				{:else if metrics}
					<div class="mb-2">
						<div class="flex justify-between text-sm mb-1">
							<span>{metrics.memory_used_mb} MB used</span>
							<span>{metrics.memory_total_mb} MB allocated</span>
						</div>
						<div class="h-3 bg-[--color-bg-tertiary] rounded-full overflow-hidden">
							<div
								class="h-full bg-[--color-warning] transition-all"
								style="width: {Math.min((metrics.memory_used_mb / metrics.memory_total_mb) * 100, 100)}%"
							></div>
						</div>
					</div>
					<div class="text-sm text-[--color-text-muted]">
						{((metrics.memory_used_mb / metrics.memory_total_mb) * 100).toFixed(1)}% of allocated memory
					</div>
				{/if}
			</div>

			<!-- System Info -->
			<div class="card">
				<h3 class="text-sm font-semibold text-[--color-text-secondary] mb-4">System</h3>
				{#if loading}
					<Skeleton height="1.5rem" class="mb-2" />
					<Skeleton height="0.75rem" width="40%" />
				{:else if metrics}
					<div class="grid grid-cols-2 gap-4">
						<div>
							<div class="text-2xl font-bold">{metrics.num_cpu}</div>
							<div class="text-sm text-[--color-text-muted]">CPU Cores</div>
						</div>
						<div>
							<div class="text-2xl font-bold">{metrics.num_goroutines}</div>
							<div class="text-sm text-[--color-text-muted]">Goroutines</div>
						</div>
					</div>
				{/if}
			</div>
		</div>

		<!-- Database Stats -->
		<div class="card">
			<h3 class="text-sm font-semibold text-[--color-text-secondary] mb-4">Database Statistics</h3>
			{#if loading}
				<div class="grid grid-cols-2 md:grid-cols-4 gap-6">
					{#each Array(4) as _}
						<div>
							<Skeleton height="2rem" width="4rem" class="mb-1" />
							<Skeleton height="1rem" width="5rem" />
						</div>
					{/each}
				</div>
			{:else if metrics}
				<div class="grid grid-cols-2 md:grid-cols-4 gap-6">
					<div>
						<div class="text-2xl font-bold">{metrics.node_count.toLocaleString()}</div>
						<div class="text-sm text-[--color-text-muted]">Total Nodes</div>
					</div>
					<div>
						<div class="text-2xl font-bold">{metrics.edge_count.toLocaleString()}</div>
						<div class="text-sm text-[--color-text-muted]">Total Edges</div>
					</div>
					<div>
						<div class="text-2xl font-bold">
							{metrics.node_count > 0 ? (metrics.edge_count / metrics.node_count).toFixed(2) : '0'}
						</div>
						<div class="text-sm text-[--color-text-muted]">Avg Degree</div>
					</div>
					<div>
						<div class="text-2xl font-bold">{metrics.total_queries.toLocaleString()}</div>
						<div class="text-sm text-[--color-text-muted]">Total Queries</div>
					</div>
				</div>
			{/if}
		</div>
	{/if}
</div>
