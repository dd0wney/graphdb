<script lang="ts">
	import { onMount } from 'svelte';
	import { isAdmin, auth } from '$lib/stores/auth';
	import { toast } from '$lib/stores/toast';
	import api from '$lib/api/client';
	import type { User, UserRole } from '$lib/api/types';
	import { Icon, Modal, ConfirmDialog, DataTable } from '$lib/components';

	let users = $state<User[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Modal states
	let showCreateModal = $state(false);
	let showEditModal = $state(false);
	let showPasswordModal = $state(false);
	let showDeleteDialog = $state(false);

	// Form states
	let selectedUser = $state<User | null>(null);
	let createForm = $state({ username: '', password: '', confirmPassword: '', role: 'viewer' as UserRole });
	let editRole = $state<UserRole>('viewer');
	let newPassword = $state('');
	let confirmNewPassword = $state('');
	let submitting = $state(false);

	const roles: { value: UserRole; label: string; description: string }[] = [
		{ value: 'admin', label: 'Admin', description: 'Full access to all features' },
		{ value: 'editor', label: 'Editor', description: 'Read/write data operations' },
		{ value: 'viewer', label: 'Viewer', description: 'Read-only access' }
	];

	const columns = [
		{ key: 'username', label: 'Username', sortable: true },
		{
			key: 'role',
			label: 'Role',
			sortable: true,
			render: (row: User) => row.role.charAt(0).toUpperCase() + row.role.slice(1)
		},
		{
			key: 'created_at',
			label: 'Created',
			sortable: true,
			render: (row: User) => {
				if (!row.created_at) return 'N/A';
				return new Date(row.created_at * 1000).toLocaleDateString();
			}
		}
	];

	onMount(async () => {
		await loadUsers();
	});

	async function loadUsers() {
		loading = true;
		error = null;
		try {
			users = await api.listUsers();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load users';
			toast.error(error);
		} finally {
			loading = false;
		}
	}

	async function handleCreateUser() {
		if (createForm.password !== createForm.confirmPassword) {
			toast.error('Passwords do not match');
			return;
		}

		submitting = true;
		try {
			await api.createUser({
				username: createForm.username,
				password: createForm.password,
				role: createForm.role
			});
			toast.success(`User "${createForm.username}" created successfully`);
			showCreateModal = false;
			createForm = { username: '', password: '', confirmPassword: '', role: 'viewer' };
			await loadUsers();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Failed to create user');
		} finally {
			submitting = false;
		}
	}

	function openEditModal(user: User) {
		selectedUser = user;
		editRole = user.role;
		showEditModal = true;
	}

	async function handleUpdateRole() {
		if (!selectedUser) return;

		submitting = true;
		try {
			await api.updateUser(selectedUser.id, editRole);
			toast.success(`Role updated for "${selectedUser.username}"`);
			showEditModal = false;
			await loadUsers();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Failed to update role');
		} finally {
			submitting = false;
		}
	}

	function openPasswordModal(user: User) {
		selectedUser = user;
		newPassword = '';
		confirmNewPassword = '';
		showPasswordModal = true;
	}

	async function handleChangePassword() {
		if (!selectedUser) return;

		if (newPassword !== confirmNewPassword) {
			toast.error('Passwords do not match');
			return;
		}

		if (newPassword.length < 8) {
			toast.error('Password must be at least 8 characters');
			return;
		}

		submitting = true;
		try {
			await api.changeUserPassword(selectedUser.id, newPassword);
			toast.success(`Password changed for "${selectedUser.username}"`);
			showPasswordModal = false;
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Failed to change password');
		} finally {
			submitting = false;
		}
	}

	function openDeleteDialog(user: User) {
		selectedUser = user;
		showDeleteDialog = true;
	}

	async function handleDeleteUser() {
		if (!selectedUser) return;

		try {
			await api.deleteUser(selectedUser.id);
			toast.success(`User "${selectedUser.username}" deleted`);
			showDeleteDialog = false;
			await loadUsers();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Failed to delete user');
		}
	}

	function getRoleBadgeClass(role: UserRole): string {
		switch (role) {
			case 'admin':
				return 'bg-red-500/20 text-red-400';
			case 'editor':
				return 'bg-blue-500/20 text-blue-400';
			default:
				return 'bg-gray-500/20 text-gray-400';
		}
	}
</script>

<div class="max-w-6xl">
	<header class="flex justify-between items-start mb-8">
		<div>
			<h1 class="text-2xl font-bold mb-1">User Management</h1>
			<p class="text-[--color-text-secondary]">Manage database users and permissions</p>
		</div>
		{#if $isAdmin}
			<button class="btn btn-primary flex items-center gap-2" onclick={() => (showCreateModal = true)}>
				<Icon name="user-plus" size={18} />
				Add User
			</button>
		{/if}
	</header>

	{#if error && !loading}
		<div class="card p-6 text-center">
			<Icon name="exclamation-circle" size={48} class="mx-auto mb-4 text-red-500" />
			<p class="text-[--color-text-secondary] mb-4">{error}</p>
			<button class="btn btn-secondary" onclick={loadUsers}>Try Again</button>
		</div>
	{:else}
		<div class="card">
			<DataTable
				data={users}
				{columns}
				{loading}
				sortable
				emptyMessage="No users found"
				emptyIcon="users"
			>
				{#snippet rowActions(user: User)}
					<div class="flex items-center gap-1">
						<button
							class="btn btn-ghost btn-icon"
							title="Edit role"
							onclick={() => openEditModal(user)}
							disabled={user.id === $auth.user?.id}
						>
							<Icon name="pencil" size={16} />
						</button>
						<button
							class="btn btn-ghost btn-icon"
							title="Change password"
							onclick={() => openPasswordModal(user)}
						>
							<Icon name="key" size={16} />
						</button>
						<button
							class="btn btn-ghost btn-icon text-red-500"
							title="Delete user"
							onclick={() => openDeleteDialog(user)}
							disabled={user.id === $auth.user?.id}
						>
							<Icon name="trash" size={16} />
						</button>
					</div>
				{/snippet}

				{#snippet emptyAction()}
					<button class="btn btn-primary" onclick={() => (showCreateModal = true)}>
						Create First User
					</button>
				{/snippet}
			</DataTable>
		</div>
	{/if}

	<!-- Roles Reference -->
	<section class="card mt-6">
		<h2 class="text-base font-semibold text-[--color-text-secondary] mb-5">Available Roles</h2>
		<div class="grid grid-cols-1 lg:grid-cols-3 gap-4">
			{#each roles as role}
				<div class="p-5 bg-[--color-bg-tertiary] rounded-[--radius]">
					<div class="flex items-center gap-2 mb-3">
						<span class="px-2 py-0.5 rounded text-xs font-medium {getRoleBadgeClass(role.value)}">
							{role.label}
						</span>
					</div>
					<p class="text-sm text-[--color-text-secondary]">{role.description}</p>
				</div>
			{/each}
		</div>
	</section>
</div>

<!-- Create User Modal -->
<Modal bind:open={showCreateModal} title="Create User" onClose={() => (showCreateModal = false)}>
	<form onsubmit={(e) => { e.preventDefault(); handleCreateUser(); }} class="space-y-4">
		<div>
			<label for="username" class="block text-sm font-medium mb-1">Username</label>
			<input
				id="username"
				type="text"
				class="input w-full"
				bind:value={createForm.username}
				placeholder="Enter username"
				required
				minlength="3"
				maxlength="50"
			/>
		</div>

		<div>
			<label for="password" class="block text-sm font-medium mb-1">Password</label>
			<input
				id="password"
				type="password"
				class="input w-full"
				bind:value={createForm.password}
				placeholder="Enter password"
				required
				minlength="8"
			/>
		</div>

		<div>
			<label for="confirmPassword" class="block text-sm font-medium mb-1">Confirm Password</label>
			<input
				id="confirmPassword"
				type="password"
				class="input w-full"
				bind:value={createForm.confirmPassword}
				placeholder="Confirm password"
				required
			/>
		</div>

		<div>
			<label for="role" class="block text-sm font-medium mb-1">Role</label>
			<select id="role" class="input w-full" bind:value={createForm.role}>
				{#each roles as role}
					<option value={role.value}>{role.label} - {role.description}</option>
				{/each}
			</select>
		</div>
	</form>

	{#snippet footer()}
		<button class="btn btn-secondary" onclick={() => (showCreateModal = false)} disabled={submitting}>
			Cancel
		</button>
		<button class="btn btn-primary" onclick={handleCreateUser} disabled={submitting}>
			{#if submitting}
				<span class="inline-block w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin mr-2"></span>
			{/if}
			Create User
		</button>
	{/snippet}
</Modal>

<!-- Edit Role Modal -->
<Modal bind:open={showEditModal} title="Edit User Role" onClose={() => (showEditModal = false)} size="sm">
	{#if selectedUser}
		<div class="space-y-4">
			<p class="text-[--color-text-secondary]">
				Change role for <strong>{selectedUser.username}</strong>
			</p>

			<div>
				<label for="editRole" class="block text-sm font-medium mb-1">Role</label>
				<select id="editRole" class="input w-full" bind:value={editRole}>
					{#each roles as role}
						<option value={role.value}>{role.label}</option>
					{/each}
				</select>
			</div>
		</div>
	{/if}

	{#snippet footer()}
		<button class="btn btn-secondary" onclick={() => (showEditModal = false)} disabled={submitting}>
			Cancel
		</button>
		<button class="btn btn-primary" onclick={handleUpdateRole} disabled={submitting}>
			{#if submitting}
				<span class="inline-block w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin mr-2"></span>
			{/if}
			Save Changes
		</button>
	{/snippet}
</Modal>

<!-- Change Password Modal -->
<Modal bind:open={showPasswordModal} title="Change Password" onClose={() => (showPasswordModal = false)} size="sm">
	{#if selectedUser}
		<div class="space-y-4">
			<p class="text-[--color-text-secondary]">
				Set new password for <strong>{selectedUser.username}</strong>
			</p>

			<div>
				<label for="newPassword" class="block text-sm font-medium mb-1">New Password</label>
				<input
					id="newPassword"
					type="password"
					class="input w-full"
					bind:value={newPassword}
					placeholder="Enter new password"
					minlength="8"
				/>
			</div>

			<div>
				<label for="confirmNewPassword" class="block text-sm font-medium mb-1">Confirm Password</label>
				<input
					id="confirmNewPassword"
					type="password"
					class="input w-full"
					bind:value={confirmNewPassword}
					placeholder="Confirm new password"
				/>
			</div>
		</div>
	{/if}

	{#snippet footer()}
		<button class="btn btn-secondary" onclick={() => (showPasswordModal = false)} disabled={submitting}>
			Cancel
		</button>
		<button class="btn btn-primary" onclick={handleChangePassword} disabled={submitting}>
			{#if submitting}
				<span class="inline-block w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin mr-2"></span>
			{/if}
			Change Password
		</button>
	{/snippet}
</Modal>

<!-- Delete Confirmation -->
<ConfirmDialog
	bind:open={showDeleteDialog}
	title="Delete User"
	message={selectedUser ? `Are you sure you want to delete "${selectedUser.username}"? This action cannot be undone.` : ''}
	confirmText="Delete"
	variant="danger"
	onConfirm={handleDeleteUser}
	onCancel={() => (showDeleteDialog = false)}
/>
