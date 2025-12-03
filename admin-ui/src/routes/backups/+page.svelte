<script lang="ts">
	import { onMount } from 'svelte';
	import { toast } from '$lib/stores/toast';
	import Skeleton from '$lib/components/Skeleton.svelte';

	interface Backup {
		id: string;
		name: string;
		created_at: string;
		size_bytes: number;
		type: 'full' | 'incremental';
		status: 'completed' | 'in_progress' | 'failed';
		node_count: number;
		edge_count: number;
	}

	let backups = $state<Backup[]>([]);
	let loading = $state(true);
	let creating = $state(false);
	let restoring = $state<string | null>(null);

	onMount(() => {
		loadBackups();
	});

	async function loadBackups() {
		loading = true;
		// Simulate API call
		await new Promise(resolve => setTimeout(resolve, 500));

		backups = [
			{
				id: '1',
				name: 'backup-2024-01-15-auto',
				created_at: '2024-01-15T08:00:00Z',
				size_bytes: 256 * 1024 * 1024,
				type: 'full',
				status: 'completed',
				node_count: 15420,
				edge_count: 48920
			},
			{
				id: '2',
				name: 'backup-2024-01-14-manual',
				created_at: '2024-01-14T15:30:00Z',
				size_bytes: 248 * 1024 * 1024,
				type: 'full',
				status: 'completed',
				node_count: 15200,
				edge_count: 47800
			},
			{
				id: '3',
				name: 'backup-2024-01-13-auto',
				created_at: '2024-01-13T08:00:00Z',
				size_bytes: 12 * 1024 * 1024,
				type: 'incremental',
				status: 'completed',
				node_count: 200,
				edge_count: 580
			}
		];
		loading = false;
	}

	async function createBackup(type: 'full' | 'incremental') {
		creating = true;
		try {
			await new Promise(resolve => setTimeout(resolve, 2000));
			toast.success(`${type === 'full' ? 'Full' : 'Incremental'} backup started`);
			await loadBackups();
		} catch {
			toast.error('Failed to create backup');
		} finally {
			creating = false;
		}
	}

	async function restoreBackup(backup: Backup) {
		if (!confirm(`Are you sure you want to restore from "${backup.name}"? This will overwrite current data.`)) {
			return;
		}

		restoring = backup.id;
		try {
			await new Promise(resolve => setTimeout(resolve, 3000));
			toast.success('Backup restored successfully');
		} catch {
			toast.error('Failed to restore backup');
		} finally {
			restoring = null;
		}
	}

	async function deleteBackup(backup: Backup) {
		if (!confirm(`Are you sure you want to delete "${backup.name}"?`)) {
			return;
		}

		try {
			backups = backups.filter(b => b.id !== backup.id);
			toast.success('Backup deleted');
		} catch {
			toast.error('Failed to delete backup');
		}
	}

	function formatSize(bytes: number): string {
		if (bytes < 1024) return `${bytes} B`;
		if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
		if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
		return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
	}

	function formatDate(dateStr: string): string {
		return new Date(dateStr).toLocaleString('en-US', {
			year: 'numeric',
			month: 'short',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit'
		});
	}
</script>

<div class="max-w-6xl">
	<header class="flex justify-between items-start mb-8">
		<div>
			<h1 class="text-2xl font-bold mb-1">Backup Management</h1>
			<p class="text-[--color-text-secondary]">Create and restore database backups</p>
		</div>
		<div class="flex gap-2">
			<button
				class="btn btn-secondary"
				onclick={() => createBackup('incremental')}
				disabled={creating}
			>
				+ Incremental Backup
			</button>
			<button
				class="btn btn-primary"
				onclick={() => createBackup('full')}
				disabled={creating}
			>
				{creating ? 'Creating...' : '+ Full Backup'}
			</button>
		</div>
	</header>

	<!-- Backup Schedule -->
	<section class="card mb-6">
		<h2 class="text-sm font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-4">Automatic Backups</h2>
		<div class="grid grid-cols-1 md:grid-cols-3 gap-6">
			<div class="flex items-center justify-between p-4 bg-[--color-bg-tertiary] rounded-lg">
				<div>
					<div class="font-medium">Daily Full Backup</div>
					<div class="text-sm text-[--color-text-muted]">Every day at 8:00 AM</div>
				</div>
				<span class="badge badge-success">Active</span>
			</div>
			<div class="flex items-center justify-between p-4 bg-[--color-bg-tertiary] rounded-lg">
				<div>
					<div class="font-medium">Hourly Incremental</div>
					<div class="text-sm text-[--color-text-muted]">Every hour</div>
				</div>
				<span class="badge badge-success">Active</span>
			</div>
			<div class="flex items-center justify-between p-4 bg-[--color-bg-tertiary] rounded-lg">
				<div>
					<div class="font-medium">Retention</div>
					<div class="text-sm text-[--color-text-muted]">Keep last 30 days</div>
				</div>
				<a href="/settings" class="text-[--color-primary] text-sm">Configure</a>
			</div>
		</div>
	</section>

	<!-- Backup List -->
	<section class="card">
		<h2 class="text-sm font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-4">Backup History</h2>

		{#if loading}
			<div class="space-y-4">
				{#each Array(3) as _}
					<Skeleton height="5rem" />
				{/each}
			</div>
		{:else if backups.length === 0}
			<div class="text-center py-12 text-[--color-text-muted]">
				<p>No backups found</p>
				<button class="btn btn-primary mt-4" onclick={() => createBackup('full')}>
					Create First Backup
				</button>
			</div>
		{:else}
			<div class="space-y-3">
				{#each backups as backup}
					<div class="flex items-center justify-between p-4 bg-[--color-bg-tertiary] rounded-lg">
						<div class="flex items-center gap-4">
							<div class="w-10 h-10 rounded-lg bg-[--color-bg] flex items-center justify-center text-xl">
								💾
							</div>
							<div>
								<div class="font-medium">{backup.name}</div>
								<div class="text-sm text-[--color-text-muted]">
									{formatDate(backup.created_at)} • {formatSize(backup.size_bytes)}
								</div>
							</div>
						</div>

						<div class="flex items-center gap-4">
							<div class="text-right text-sm">
								<div>{backup.node_count.toLocaleString()} nodes</div>
								<div class="text-[--color-text-muted]">{backup.edge_count.toLocaleString()} edges</div>
							</div>

							<span class="badge {backup.type === 'full' ? 'badge-info' : 'badge-warning'}">
								{backup.type}
							</span>

							<span class="badge badge-{backup.status === 'completed' ? 'success' : backup.status === 'failed' ? 'error' : 'warning'}">
								{backup.status}
							</span>

							<div class="flex gap-2">
								<button
									class="btn btn-sm btn-secondary"
									onclick={() => restoreBackup(backup)}
									disabled={restoring === backup.id || backup.status !== 'completed'}
								>
									{restoring === backup.id ? 'Restoring...' : 'Restore'}
								</button>
								<button
									class="btn btn-sm btn-ghost text-[--color-error]"
									onclick={() => deleteBackup(backup)}
								>
									Delete
								</button>
							</div>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</section>

	<!-- Storage Info -->
	<section class="card mt-6">
		<h2 class="text-sm font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-4">Storage</h2>
		<div class="grid grid-cols-1 md:grid-cols-3 gap-6">
			<div>
				<div class="text-2xl font-bold">{formatSize(backups.reduce((sum, b) => sum + b.size_bytes, 0))}</div>
				<div class="text-sm text-[--color-text-muted]">Total Backup Size</div>
			</div>
			<div>
				<div class="text-2xl font-bold">{backups.length}</div>
				<div class="text-sm text-[--color-text-muted]">Total Backups</div>
			</div>
			<div>
				<div class="text-2xl font-bold">{backups.filter(b => b.status === 'completed').length}</div>
				<div class="text-sm text-[--color-text-muted]">Successful Backups</div>
			</div>
		</div>
	</section>
</div>
