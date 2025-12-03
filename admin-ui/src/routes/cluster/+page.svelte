<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import type { HealthResponse } from '$lib/api/types';

	let health = $state<HealthResponse | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let refreshing = $state(false);

	onMount(async () => {
		await loadClusterStatus();
	});

	async function loadClusterStatus() {
		loading = true;
		error = null;
		try {
			health = await api.getHealth();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load cluster status';
		} finally {
			loading = false;
		}
	}

	async function refresh() {
		refreshing = true;
		await loadClusterStatus();
		refreshing = false;
	}

	function getStatusColor(status: string): string {
		switch (status?.toLowerCase()) {
			case 'healthy':
			case 'ok':
			case 'connected':
				return 'success';
			case 'degraded':
				return 'warning';
			default:
				return 'error';
		}
	}
</script>

<div class="max-w-6xl">
	<header class="flex justify-between items-start mb-8">
		<div>
			<h1 class="text-2xl font-bold mb-1">Cluster Status</h1>
			<p class="text-[--color-text-secondary]">Monitor node health and replication status</p>
		</div>
		<button class="btn btn-secondary" onclick={refresh} disabled={refreshing}>
			{refreshing ? 'Refreshing...' : 'Refresh'}
		</button>
	</header>

	{#if error}
		<div class="alert alert-error">{error}</div>
	{/if}

	{#if loading}
		<div class="flex flex-col items-center py-16 gap-4 text-[--color-text-secondary]">
			<div class="spinner"></div>
			<p>Loading cluster status...</p>
		</div>
	{:else if health}
		<div class="grid grid-cols-1 lg:grid-cols-2 gap-6 mb-6">
			<!-- Current Node Status -->
			<section class="card">
				<div class="flex justify-between items-center mb-4">
					<h2 class="text-sm font-semibold text-[--color-text-secondary] uppercase tracking-wider">Current Node</h2>
					<span class="badge badge-{getStatusColor(health.status)}">{health.status}</span>
				</div>
				<div class="flex flex-col gap-3">
					<div class="flex justify-between items-center">
						<span class="text-sm text-[--color-text-secondary]">Version</span>
						<span class="font-medium">{health.version || 'N/A'}</span>
					</div>
					<div class="flex justify-between items-center">
						<span class="text-sm text-[--color-text-secondary]">Edition</span>
						<span class="font-medium">{health.edition || 'Community'}</span>
					</div>
					<div class="flex justify-between items-center">
						<span class="text-sm text-[--color-text-secondary]">Uptime</span>
						<span class="font-medium">{health.uptime || 'N/A'}</span>
					</div>
					<div class="flex justify-between items-center">
						<span class="text-sm text-[--color-text-secondary]">Last Check</span>
						<span class="font-medium">{new Date(health.timestamp).toLocaleString()}</span>
					</div>
				</div>
			</section>

			<!-- Health Checks -->
			<section class="card">
				<h2 class="text-sm font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-4">Health Checks</h2>
				{#if health.checks && Object.keys(health.checks).length > 0}
					<div class="flex flex-col gap-3">
						{#each Object.entries(health.checks) as [name, status]}
							<div class="flex justify-between items-center">
								<div class="flex items-center gap-2">
									<span class="status-dot {getStatusColor(String(status))}"></span>
									<span class="capitalize">{name}</span>
								</div>
								<span class="badge badge-{getStatusColor(String(status))}">
									{String(status)}
								</span>
							</div>
						{/each}
					</div>
				{:else}
					<p class="text-sm text-[--color-text-muted]">No health checks configured</p>
				{/if}
			</section>

			<!-- Features -->
			<section class="card">
				<h2 class="text-sm font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-4">Enabled Features</h2>
				{#if health.features && health.features.length > 0}
					<div class="flex flex-wrap gap-2">
						{#each health.features as feature}
							<div class="flex items-center gap-1 px-3 py-1.5 bg-[--color-bg-tertiary] rounded-[--radius] text-sm">
								<span class="text-[--color-success]">✓</span>
								{feature}
							</div>
						{/each}
					</div>
				{:else}
					<p class="text-sm text-[--color-text-muted]">No features reported</p>
				{/if}
			</section>

			<!-- Cluster Info Placeholder -->
			<section class="card">
				<h2 class="text-sm font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-4">Replication</h2>
				<div class="flex flex-col gap-3">
					<p class="text-sm text-[--color-text-muted] pb-3 border-b border-[--color-border]">
						Replication status is available when running in cluster mode.
					</p>
					<div class="flex justify-between items-center">
						<span class="text-sm text-[--color-text-secondary]">Mode</span>
						<span class="font-medium">Standalone</span>
					</div>
					<div class="flex justify-between items-center">
						<span class="text-sm text-[--color-text-secondary]">Connected Replicas</span>
						<span class="font-medium">0</span>
					</div>
				</div>
			</section>
		</div>

		<!-- System Info -->
		<section class="card">
			<h2 class="text-sm font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-4">System Information</h2>
			<div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
				<div class="flex items-center gap-3 p-4 bg-[--color-bg-tertiary] rounded-[--radius]">
					<div class="text-2xl">🖥️</div>
					<div class="flex flex-col">
						<span class="text-xs text-[--color-text-muted]">Node Type</span>
						<span class="font-medium">Primary</span>
					</div>
				</div>
				<div class="flex items-center gap-3 p-4 bg-[--color-bg-tertiary] rounded-[--radius]">
					<div class="text-2xl">🔒</div>
					<div class="flex flex-col">
						<span class="text-xs text-[--color-text-muted]">TLS</span>
						<span class="font-medium">Configured</span>
					</div>
				</div>
				<div class="flex items-center gap-3 p-4 bg-[--color-bg-tertiary] rounded-[--radius]">
					<div class="text-2xl">🔐</div>
					<div class="flex flex-col">
						<span class="text-xs text-[--color-text-muted]">Encryption</span>
						<span class="font-medium">At Rest</span>
					</div>
				</div>
				<div class="flex items-center gap-3 p-4 bg-[--color-bg-tertiary] rounded-[--radius]">
					<div class="text-2xl">📊</div>
					<div class="flex flex-col">
						<span class="text-xs text-[--color-text-muted]">Metrics</span>
						<span class="font-medium">Prometheus</span>
					</div>
				</div>
			</div>
		</section>
	{/if}
</div>
