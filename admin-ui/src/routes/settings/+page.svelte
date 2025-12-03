<script lang="ts">
	import { auth } from '$lib/stores/auth';
	import { theme } from '$lib/stores/theme';
	import { toast } from '$lib/stores/toast';

	let currentPassword = $state('');
	let newPassword = $state('');
	let confirmPassword = $state('');
	let changingPassword = $state(false);

	let apiUrl = $state('http://localhost:8080');
	let sessionTimeout = $state(30);
	let autoRefreshInterval = $state(5);

	async function changePassword() {
		if (newPassword !== confirmPassword) {
			toast.error('Passwords do not match');
			return;
		}

		if (newPassword.length < 8) {
			toast.error('Password must be at least 8 characters');
			return;
		}

		changingPassword = true;
		try {
			// In real app, call API to change password
			await new Promise(resolve => setTimeout(resolve, 1000));
			toast.success('Password changed successfully');
			currentPassword = '';
			newPassword = '';
			confirmPassword = '';
		} catch (err) {
			toast.error('Failed to change password');
		} finally {
			changingPassword = false;
		}
	}

	function saveSettings() {
		// Save to localStorage
		localStorage.setItem('settings', JSON.stringify({
			apiUrl,
			sessionTimeout,
			autoRefreshInterval
		}));
		toast.success('Settings saved');
	}

	function exportSettings() {
		const settings = {
			apiUrl,
			sessionTimeout,
			autoRefreshInterval,
			theme: $theme
		};
		const blob = new Blob([JSON.stringify(settings, null, 2)], { type: 'application/json' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = 'graphdb-settings.json';
		a.click();
		URL.revokeObjectURL(url);
		toast.success('Settings exported');
	}
</script>

<div class="max-w-4xl">
	<header class="page-header">
		<h1>Settings</h1>
		<p class="subtitle">Configure your dashboard preferences</p>
	</header>

	<!-- Account Section -->
	<section class="card mb-6">
		<h2 class="text-lg font-semibold mb-4">Account</h2>

		<div class="grid grid-cols-1 md:grid-cols-2 gap-6 mb-6">
			<div>
				<label class="label">Username</label>
				<input type="text" class="input" value={$auth.user?.username || ''} disabled />
			</div>
			<div>
				<label class="label">Role</label>
				<input type="text" class="input capitalize" value={$auth.user?.role || ''} disabled />
			</div>
		</div>

		<h3 class="text-sm font-semibold text-[--color-text-secondary] mb-4">Change Password</h3>
		<div class="grid grid-cols-1 md:grid-cols-3 gap-4 mb-4">
			<div>
				<label class="label" for="currentPassword">Current Password</label>
				<input
					type="password"
					id="currentPassword"
					class="input"
					bind:value={currentPassword}
					placeholder="Current password"
				/>
			</div>
			<div>
				<label class="label" for="newPassword">New Password</label>
				<input
					type="password"
					id="newPassword"
					class="input"
					bind:value={newPassword}
					placeholder="New password"
				/>
			</div>
			<div>
				<label class="label" for="confirmPassword">Confirm Password</label>
				<input
					type="password"
					id="confirmPassword"
					class="input"
					bind:value={confirmPassword}
					placeholder="Confirm password"
				/>
			</div>
		</div>
		<button
			class="btn btn-primary"
			onclick={changePassword}
			disabled={changingPassword || !currentPassword || !newPassword}
		>
			{changingPassword ? 'Changing...' : 'Change Password'}
		</button>
	</section>

	<!-- Appearance Section -->
	<section class="card mb-6">
		<h2 class="text-lg font-semibold mb-4">Appearance</h2>

		<div class="flex items-center justify-between py-3 border-b border-[--color-border]">
			<div>
				<div class="font-medium">Theme</div>
				<div class="text-sm text-[--color-text-muted]">Choose between dark and light mode</div>
			</div>
			<div class="flex gap-2">
				<button
					class="btn {$theme === 'dark' ? 'btn-primary' : 'btn-secondary'}"
					onclick={() => theme.set('dark')}
				>
					🌙 Dark
				</button>
				<button
					class="btn {$theme === 'light' ? 'btn-primary' : 'btn-secondary'}"
					onclick={() => theme.set('light')}
				>
					☀️ Light
				</button>
			</div>
		</div>
	</section>

	<!-- Connection Section -->
	<section class="card mb-6">
		<h2 class="text-lg font-semibold mb-4">Connection</h2>

		<div class="space-y-4">
			<div>
				<label class="label" for="apiUrl">API URL</label>
				<input
					type="url"
					id="apiUrl"
					class="input"
					bind:value={apiUrl}
					placeholder="http://localhost:8080"
				/>
				<p class="text-xs text-[--color-text-muted] mt-1">The GraphDB server endpoint</p>
			</div>

			<div>
				<label class="label" for="sessionTimeout">Session Timeout (minutes)</label>
				<input
					type="number"
					id="sessionTimeout"
					class="input"
					bind:value={sessionTimeout}
					min="5"
					max="1440"
				/>
			</div>

			<div>
				<label class="label" for="autoRefresh">Auto-refresh Interval (seconds)</label>
				<input
					type="number"
					id="autoRefresh"
					class="input"
					bind:value={autoRefreshInterval}
					min="1"
					max="60"
				/>
			</div>
		</div>

		<div class="flex gap-3 mt-6">
			<button class="btn btn-primary" onclick={saveSettings}>
				Save Settings
			</button>
			<button class="btn btn-secondary" onclick={exportSettings}>
				Export Settings
			</button>
		</div>
	</section>

	<!-- Security Section -->
	<section class="card mb-6">
		<h2 class="text-lg font-semibold mb-4">Security</h2>

		<div class="space-y-4">
			<div class="flex items-center justify-between p-4 bg-[--color-bg-tertiary] rounded-lg">
				<div>
					<div class="font-medium">Active Sessions</div>
					<div class="text-sm text-[--color-text-muted]">Manage your login sessions across devices</div>
				</div>
				<a href="/sessions" class="btn btn-secondary">
					View Sessions
				</a>
			</div>

			<div class="flex items-center justify-between p-4 bg-[--color-bg-tertiary] rounded-lg">
				<div>
					<div class="font-medium">Two-Factor Authentication</div>
					<div class="text-sm text-[--color-text-muted]">Add an extra layer of security to your account</div>
				</div>
				<span class="badge badge-warning">Coming Soon</span>
			</div>
		</div>
	</section>

	<!-- Keyboard Shortcuts -->
	<section class="card mb-6">
		<h2 class="text-lg font-semibold mb-4">Keyboard Shortcuts</h2>

		<div class="space-y-2">
			<div class="flex justify-between items-center py-2 border-b border-[--color-border]">
				<span>Open Command Palette</span>
				<kbd>⌘ K</kbd>
			</div>
			<div class="flex justify-between items-center py-2 border-b border-[--color-border]">
				<span>Execute Query (in Explorer)</span>
				<kbd>⌘ Enter</kbd>
			</div>
			<div class="flex justify-between items-center py-2 border-b border-[--color-border]">
				<span>Toggle Theme</span>
				<span class="text-[--color-text-muted]">Use sidebar toggle</span>
			</div>
			<div class="flex justify-between items-center py-2">
				<span>Close Modal / Cancel</span>
				<kbd>Esc</kbd>
			</div>
		</div>
	</section>

	<!-- About -->
	<section class="card">
		<h2 class="text-lg font-semibold mb-4">About</h2>

		<div class="space-y-2 text-sm">
			<div class="flex justify-between">
				<span class="text-[--color-text-muted]">Dashboard Version</span>
				<span>1.0.0</span>
			</div>
			<div class="flex justify-between">
				<span class="text-[--color-text-muted]">Built with</span>
				<span>SvelteKit, Tailwind CSS, D3.js</span>
			</div>
			<div class="flex justify-between">
				<span class="text-[--color-text-muted]">Documentation</span>
				<a href="/docs" class="text-[--color-primary]">View Docs</a>
			</div>
		</div>
	</section>
</div>
