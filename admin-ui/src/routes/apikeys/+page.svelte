<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import { isAdmin } from '$lib/stores/auth';
	import { toast } from '$lib/stores/toast';
	import type { APIKey, CreateAPIKeyRequest } from '$lib/api/types';
	import { Icon, Modal, ConfirmDialog, DataTable } from '$lib/components';

	let keys = $state<APIKey[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Modal states
	let showCreateModal = $state(false);
	let showRevokeDialog = $state(false);
	let showKeyResult = $state(false);

	// Selected key for actions
	let selectedKey = $state<APIKey | null>(null);
	let newKeyResult = $state<{ key: string; apiKey: APIKey } | null>(null);

	// Create form state
	let newKeyName = $state('');
	let newKeyExpiry = $state('never');
	let newKeyPermissions = $state<string[]>(['read']);
	let creating = $state(false);

	const permissionOptions = [
		{ value: 'read', label: 'Read', description: 'Query data' },
		{ value: 'write', label: 'Write', description: 'Create/update data' },
		{ value: 'admin', label: 'Admin', description: 'Full access' }
	];

	const expiryOptions = [
		{ value: 'never', label: 'Never expires' },
		{ value: '3600', label: '1 hour' },
		{ value: '86400', label: '1 day' },
		{ value: '604800', label: '7 days' },
		{ value: '2592000', label: '30 days' },
		{ value: '31536000', label: '1 year' }
	];

	const columns = [
		{ key: 'name', label: 'Name', sortable: true },
		{
			key: 'prefix',
			label: 'Key Prefix'
		},
		{
			key: 'permissions',
			label: 'Permissions',
			render: (row: APIKey) => row.permissions.join(', ')
		},
		{
			key: 'created_at',
			label: 'Created',
			sortable: true,
			render: (row: APIKey) => formatDate(row.created_at)
		},
		{
			key: 'expires_at',
			label: 'Expires',
			render: (row: APIKey) => formatDate(row.expires_at)
		},
		{
			key: 'status',
			label: 'Status',
			render: (row: APIKey) => getStatusLabel(row)
		}
	];

	onMount(async () => {
		await loadKeys();
	});

	async function loadKeys() {
		loading = true;
		error = null;
		try {
			const response = await api.listAPIKeys();
			keys = response.keys || [];
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load API keys';
			toast.error(error);
		} finally {
			loading = false;
		}
	}

	async function createKey() {
		if (!newKeyName.trim()) {
			toast.error('Please enter a name for the API key');
			return;
		}

		if (newKeyPermissions.length === 0) {
			toast.error('Please select at least one permission');
			return;
		}

		creating = true;

		try {
			const req: CreateAPIKeyRequest = {
				name: newKeyName.trim(),
				permissions: newKeyPermissions,
				expires_in: newKeyExpiry === 'never' ? 0 : parseInt(newKeyExpiry)
			};

			const response = await api.createAPIKey(req);
			newKeyResult = { key: response.key, apiKey: response.api_key };
			showCreateModal = false;
			showKeyResult = true;
			await loadKeys();

			// Reset form
			newKeyName = '';
			newKeyExpiry = 'never';
			newKeyPermissions = ['read'];

			toast.success('API key created successfully');
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Failed to create API key');
		} finally {
			creating = false;
		}
	}

	function openRevokeDialog(key: APIKey) {
		selectedKey = key;
		showRevokeDialog = true;
	}

	async function handleRevokeKey() {
		if (!selectedKey) return;

		try {
			await api.revokeAPIKey(selectedKey.id);
			toast.success(`API key "${selectedKey.name}" revoked`);
			showRevokeDialog = false;
			await loadKeys();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Failed to revoke API key');
		}
	}

	function togglePermission(perm: string) {
		if (newKeyPermissions.includes(perm)) {
			newKeyPermissions = newKeyPermissions.filter(p => p !== perm);
		} else {
			newKeyPermissions = [...newKeyPermissions, perm];
		}
	}

	let copiedId = $state<string | null>(null);

	async function copyToClipboard(text: string, id?: string) {
		try {
			await navigator.clipboard.writeText(text);
			toast.success('Copied to clipboard');
			if (id) {
				copiedId = id;
				setTimeout(() => (copiedId = null), 2000);
			}
		} catch {
			toast.error('Failed to copy to clipboard');
		}
	}

	function formatDate(dateStr: string | undefined): string {
		if (!dateStr) return 'Never';
		return new Date(dateStr).toLocaleDateString('en-US', {
			year: 'numeric',
			month: 'short',
			day: 'numeric'
		});
	}

	function getStatusLabel(key: APIKey): string {
		if (key.revoked) return 'Revoked';
		if (key.expires_at && new Date(key.expires_at) < new Date()) return 'Expired';
		return 'Active';
	}

	function getStatusClass(key: APIKey): string {
		if (key.revoked) return 'bg-red-500/20 text-red-400';
		if (key.expires_at && new Date(key.expires_at) < new Date()) return 'bg-yellow-500/20 text-yellow-400';
		return 'bg-green-500/20 text-green-400';
	}

	function getPermissionClass(perm: string): string {
		switch (perm) {
			case 'admin': return 'bg-red-500/20 text-red-400';
			case 'write': return 'bg-blue-500/20 text-blue-400';
			default: return 'bg-gray-500/20 text-gray-400';
		}
	}
</script>

<div class="max-w-6xl">
	<header class="flex justify-between items-start mb-8">
		<div>
			<h1 class="text-2xl font-bold mb-1">API Keys</h1>
			<p class="text-[--color-text-secondary]">Manage API keys for programmatic access</p>
		</div>
		{#if $isAdmin}
			<button class="btn btn-primary flex items-center gap-2" onclick={() => (showCreateModal = true)}>
				<Icon name="plus" size={18} />
				Create API Key
			</button>
		{/if}
	</header>

	{#if error && !loading && keys.length === 0}
		<div class="card p-6 text-center">
			<Icon name="exclamation-circle" size={48} class="mx-auto mb-4 text-red-500" />
			<p class="text-[--color-text-secondary] mb-4">{error}</p>
			<button class="btn btn-secondary" onclick={loadKeys}>Try Again</button>
		</div>
	{:else}
		<div class="card">
			<DataTable
				data={keys}
				{columns}
				{loading}
				sortable
				emptyMessage="No API keys found"
				emptyIcon="key"
				cellSlots={{
					prefix: prefixCell
				}}
			>
				{#snippet rowActions(key: APIKey)}
					<div class="flex items-center gap-2">
						<span class="px-2 py-0.5 rounded text-xs font-medium {getStatusClass(key)}">
							{getStatusLabel(key)}
						</span>
						{#if !key.revoked}
							<button
								class="btn btn-ghost btn-icon text-red-500"
								title="Revoke key"
								onclick={() => openRevokeDialog(key)}
							>
								<Icon name="trash" size={16} />
							</button>
						{/if}
					</div>
				{/snippet}

				{#snippet emptyAction()}
					{#if $isAdmin}
						<button class="btn btn-primary" onclick={() => (showCreateModal = true)}>
							Create Your First API Key
						</button>
					{/if}
				{/snippet}
			</DataTable>
		</div>

		{#snippet prefixCell(key: APIKey)}
			<div class="flex items-center gap-2 group">
				<code class="text-xs bg-[--color-bg-tertiary] px-2 py-1 rounded font-mono">{key.prefix}...</code>
				<button
					class="btn btn-ghost btn-icon opacity-0 group-hover:opacity-100 transition-opacity"
					title="Copy key prefix"
					onclick={(e) => { e.stopPropagation(); copyToClipboard(key.prefix, key.id); }}
				>
					{#if copiedId === key.id}
						<Icon name="check" size={14} class="text-green-500" />
					{:else}
						<Icon name="document-duplicate" size={14} />
					{/if}
				</button>
			</div>
		{/snippet}
	{/if}

	<!-- Usage Guide -->
	<section class="card mt-6">
		<h2 class="text-base font-semibold text-[--color-text-secondary] mb-4">Using API Keys</h2>
		<p class="text-sm text-[--color-text-secondary] mb-4">
			Include your API key in the <code class="px-1.5 py-0.5 bg-[--color-bg-tertiary] rounded">Authorization</code> header:
		</p>
		<div class="bg-[--color-bg] rounded-[--radius] p-4 overflow-x-auto">
			<code class="text-sm whitespace-pre">curl -H "Authorization: Bearer YOUR_API_KEY" \
     https://your-graphdb-server/api/query</code>
		</div>
	</section>
</div>

<!-- Create API Key Modal -->
<Modal bind:open={showCreateModal} title="Create API Key" onClose={() => (showCreateModal = false)}>
	<div class="space-y-5">
		<div>
			<label for="keyName" class="block text-sm font-medium mb-1">Name</label>
			<input
				type="text"
				id="keyName"
				class="input w-full"
				bind:value={newKeyName}
				placeholder="e.g., Production API Key"
			/>
			<p class="text-xs text-[--color-text-muted] mt-1">A descriptive name to identify this key</p>
		</div>

		<div>
			<label class="block text-sm font-medium mb-2">Permissions</label>
			<div class="space-y-2">
				{#each permissionOptions as perm}
					<label class="flex items-center gap-3 p-3 rounded-[--radius] bg-[--color-bg-tertiary] cursor-pointer hover:bg-[--color-bg]">
						<input
							type="checkbox"
							class="w-4 h-4 rounded"
							checked={newKeyPermissions.includes(perm.value)}
							onchange={() => togglePermission(perm.value)}
						/>
						<div class="flex-1">
							<span class="font-medium">{perm.label}</span>
							<span class="text-sm text-[--color-text-muted] ml-2">- {perm.description}</span>
						</div>
						<span class="px-2 py-0.5 rounded text-xs font-medium {getPermissionClass(perm.value)}">
							{perm.value}
						</span>
					</label>
				{/each}
			</div>
		</div>

		<div>
			<label for="keyExpiry" class="block text-sm font-medium mb-1">Expiration</label>
			<select id="keyExpiry" class="input w-full" bind:value={newKeyExpiry}>
				{#each expiryOptions as opt}
					<option value={opt.value}>{opt.label}</option>
				{/each}
			</select>
		</div>
	</div>

	{#snippet footer()}
		<button class="btn btn-secondary" onclick={() => (showCreateModal = false)} disabled={creating}>
			Cancel
		</button>
		<button class="btn btn-primary" onclick={createKey} disabled={creating}>
			{#if creating}
				<span class="inline-block w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin mr-2"></span>
			{/if}
			Create Key
		</button>
	{/snippet}
</Modal>

<!-- Key Result Modal -->
<Modal bind:open={showKeyResult} title="API Key Created" onClose={() => { showKeyResult = false; newKeyResult = null; }} size="lg">
	{#if newKeyResult}
		<div class="space-y-4">
			<div class="p-4 bg-yellow-500/10 border border-yellow-500/30 rounded-[--radius]">
				<div class="flex items-start gap-3">
					<Icon name="exclamation-triangle" size={24} class="text-yellow-500 shrink-0 mt-0.5" />
					<div>
						<p class="font-medium text-yellow-500">Important: Copy your API key now</p>
						<p class="text-sm text-[--color-text-secondary] mt-1">
							This is the only time you'll see this key. Store it securely - you won't be able to retrieve it later.
						</p>
					</div>
				</div>
			</div>

			<div>
				<label class="block text-sm font-medium mb-1">Your API Key</label>
				<div class="flex items-center gap-2">
					<code class="flex-1 p-3 bg-[--color-bg] rounded-[--radius] text-sm break-all font-mono">
						{newKeyResult.key}
					</code>
					<button
						class="btn btn-secondary shrink-0"
						onclick={() => copyToClipboard(newKeyResult!.key)}
					>
						<Icon name="document-duplicate" size={16} class="mr-1" />
						Copy
					</button>
				</div>
			</div>

			<div class="grid grid-cols-2 gap-4 text-sm">
				<div>
					<span class="text-[--color-text-muted]">Name:</span>
					<span class="ml-2 font-medium">{newKeyResult.apiKey.name}</span>
				</div>
				<div>
					<span class="text-[--color-text-muted]">Permissions:</span>
					<span class="ml-2 font-medium">{newKeyResult.apiKey.permissions.join(', ')}</span>
				</div>
			</div>
		</div>
	{/if}

	{#snippet footer()}
		<button class="btn btn-primary" onclick={() => { showKeyResult = false; newKeyResult = null; }}>
			Done
		</button>
	{/snippet}
</Modal>

<!-- Revoke Confirmation -->
<ConfirmDialog
	bind:open={showRevokeDialog}
	title="Revoke API Key"
	message={selectedKey ? `Are you sure you want to revoke the API key "${selectedKey.name}"? Any applications using this key will immediately lose access.` : ''}
	confirmText="Revoke"
	variant="danger"
	onConfirm={handleRevokeKey}
	onCancel={() => (showRevokeDialog = false)}
/>
