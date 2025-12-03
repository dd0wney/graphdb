<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import type { AuditEvent, AuditLogsResponse } from '$lib/api/types';

	let logs = $state<AuditEvent[]>([]);
	let total = $state(0);
	let persistentInfo = $state<AuditLogsResponse['persistent_audit'] | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let exporting = $state(false);

	// Filters
	let filterUser = $state('');
	let filterAction = $state('');
	let filterStatus = $state('');
	let filterResourceType = $state('');
	let limit = $state(100);

	const actionOptions = ['', 'create', 'read', 'update', 'delete', 'login', 'logout', 'query'];
	const statusOptions = ['', 'success', 'failure'];
	const resourceOptions = ['', 'node', 'edge', 'user', 'apikey', 'system'];

	onMount(async () => {
		await loadLogs();
	});

	async function loadLogs() {
		loading = true;
		error = null;

		try {
			const params: Record<string, string | number> = { limit };
			if (filterUser) params.username = filterUser;
			if (filterAction) params.action = filterAction;
			if (filterStatus) params.status = filterStatus;
			if (filterResourceType) params.resource_type = filterResourceType;

			const response = await api.getAuditLogs(params);
			logs = response.events || [];
			total = response.total;
			persistentInfo = response.persistent_audit || null;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load audit logs';
		} finally {
			loading = false;
		}
	}

	async function exportLogs() {
		exporting = true;
		try {
			const blob = await api.exportAuditLogs();
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = `audit-logs-${new Date().toISOString().split('T')[0]}.json`;
			document.body.appendChild(a);
			a.click();
			document.body.removeChild(a);
			URL.revokeObjectURL(url);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to export logs';
		} finally {
			exporting = false;
		}
	}

	function formatDate(dateStr: string): string {
		return new Date(dateStr).toLocaleString('en-US', {
			month: 'short',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit',
			second: '2-digit'
		});
	}

	function getStatusBadge(status: string): string {
		return status === 'success' ? 'success' : 'error';
	}

	function getActionIcon(action: string): string {
		switch (action) {
			case 'create':
				return '+';
			case 'delete':
				return '×';
			case 'update':
				return '↻';
			case 'read':
			case 'query':
				return '⊙';
			case 'login':
				return '→';
			case 'logout':
				return '←';
			default:
				return '•';
		}
	}

	function getActionColor(action: string): string {
		switch (action) {
			case 'create':
				return 'text-[--color-success]';
			case 'delete':
				return 'text-[--color-error]';
			case 'update':
				return 'text-[--color-warning]';
			case 'login':
			case 'logout':
				return 'text-[--color-primary]';
			default:
				return 'text-[--color-text-secondary]';
		}
	}

	function formatBytes(bytes: number): string {
		if (bytes < 1024) return `${bytes} B`;
		if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
		return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
	}
</script>

<div class="max-w-6xl">
	<header class="flex justify-between items-start mb-6">
		<div>
			<h1 class="text-2xl font-bold mb-1">Audit Logs</h1>
			<p class="text-[--color-text-secondary]">Security and activity audit trail</p>
		</div>
		<button class="btn btn-secondary" onclick={exportLogs} disabled={exporting}>
			{exporting ? 'Exporting...' : 'Export Logs'}
		</button>
	</header>

	<!-- Stats Bar -->
	<div class="flex gap-8 mb-6 p-4 px-6 bg-[--color-bg-secondary] border border-[--color-border] rounded-[--radius-lg]">
		<div class="flex flex-col">
			<span class="text-xl font-bold font-mono">{total.toLocaleString()}</span>
			<span class="text-xs text-[--color-text-muted] uppercase tracking-wider">Total Events</span>
		</div>
		{#if persistentInfo}
			<div class="flex flex-col">
				<span class="text-xl font-bold font-mono">{persistentInfo.total_persisted.toLocaleString()}</span>
				<span class="text-xs text-[--color-text-muted] uppercase tracking-wider">Persisted</span>
			</div>
			<div class="flex flex-col">
				<span class="text-xl font-bold font-mono">{persistentInfo.total_files}</span>
				<span class="text-xs text-[--color-text-muted] uppercase tracking-wider">Log Files</span>
			</div>
			<div class="flex flex-col">
				<span class="text-xl font-bold font-mono">{formatBytes(persistentInfo.total_size_bytes)}</span>
				<span class="text-xs text-[--color-text-muted] uppercase tracking-wider">Total Size</span>
			</div>
		{/if}
	</div>

	<!-- Filters -->
	<div class="card flex flex-wrap gap-4 items-end mb-6">
		<div class="flex-1 min-w-[140px]">
			<label class="label mb-1">Username</label>
			<input
				type="text"
				class="input"
				placeholder="Filter by username..."
				bind:value={filterUser}
				onchange={loadLogs}
			/>
		</div>

		<div class="flex-1 min-w-[140px]">
			<label class="label mb-1">Action</label>
			<select class="input" bind:value={filterAction} onchange={loadLogs}>
				{#each actionOptions as opt}
					<option value={opt}>{opt || 'All actions'}</option>
				{/each}
			</select>
		</div>

		<div class="flex-1 min-w-[140px]">
			<label class="label mb-1">Status</label>
			<select class="input" bind:value={filterStatus} onchange={loadLogs}>
				{#each statusOptions as opt}
					<option value={opt}>{opt || 'All statuses'}</option>
				{/each}
			</select>
		</div>

		<div class="flex-1 min-w-[140px]">
			<label class="label mb-1">Resource</label>
			<select class="input" bind:value={filterResourceType} onchange={loadLogs}>
				{#each resourceOptions as opt}
					<option value={opt}>{opt || 'All resources'}</option>
				{/each}
			</select>
		</div>

		<div class="flex-1 min-w-[140px]">
			<label class="label mb-1">Limit</label>
			<select class="input" bind:value={limit} onchange={loadLogs}>
				<option value={50}>50</option>
				<option value={100}>100</option>
				<option value={250}>250</option>
				<option value={500}>500</option>
			</select>
		</div>

		<button class="btn btn-secondary min-w-[80px]" onclick={loadLogs}>
			Refresh
		</button>
	</div>

	{#if error}
		<div class="alert alert-error">{error}</div>
	{/if}

	{#if loading}
		<div class="flex flex-col items-center py-16 gap-4 text-[--color-text-secondary]">
			<div class="spinner"></div>
			<p>Loading audit logs...</p>
		</div>
	{:else if logs.length === 0}
		<div class="card text-center py-12">
			<h3 class="text-[--color-text-secondary] mb-2">No Audit Events</h3>
			<p class="text-[--color-text-muted]">No audit events match your current filters.</p>
		</div>
	{:else}
		<div class="card p-0">
			<div class="flex flex-col">
				{#each logs as event}
					<div class="flex gap-4 p-4 px-6 border-b border-[--color-border] last:border-b-0">
						<div class="shrink-0 w-8 h-8 flex items-center justify-center bg-[--color-bg-tertiary] rounded-full font-semibold {getActionColor(event.action)}">
							{getActionIcon(event.action)}
						</div>

						<div class="flex-1 min-w-0">
							<div class="flex items-center gap-2 mb-1">
								<span class="font-semibold capitalize">{event.action}</span>
								<span class="text-sm text-[--color-text-secondary]">{event.resource_type}</span>
								{#if event.resource_id}
									<code class="text-xs bg-[--color-bg-tertiary] px-1.5 py-0.5 rounded-[--radius]">{event.resource_id}</code>
								{/if}
							</div>
							<div class="flex items-center gap-2 text-xs text-[--color-text-muted]">
								<span>{event.username || event.user_id}</span>
								<span class="opacity-50">•</span>
								<span>{event.ip_address}</span>
								{#if event.user_agent}
									<span class="opacity-50">•</span>
									<span title={event.user_agent}>
										{event.user_agent.split(' ')[0]}
									</span>
								{/if}
							</div>
							{#if event.details && Object.keys(event.details).length > 0}
								<details class="mt-2">
									<summary class="text-xs text-[--color-text-secondary] cursor-pointer">Details</summary>
									<pre class="text-xs mt-2 max-h-[150px] overflow-auto">{JSON.stringify(event.details, null, 2)}</pre>
								</details>
							{/if}
						</div>

						<div class="flex flex-col items-end gap-1 shrink-0">
							<span class="badge badge-{getStatusBadge(event.status)}">
								{event.status}
							</span>
							<span class="text-xs text-[--color-text-muted]">{formatDate(event.timestamp)}</span>
						</div>
					</div>
				{/each}
			</div>
		</div>

		<div class="mt-4 text-center">
			<span class="text-sm text-[--color-text-muted]">
				Showing {logs.length} of {total.toLocaleString()} events
			</span>
		</div>
	{/if}
</div>
