<script lang="ts">
	import { onMount } from 'svelte';
	import { toast } from '$lib/stores/toast';
	import Skeleton from '$lib/components/Skeleton.svelte';

	interface Session {
		id: string;
		device: string;
		browser: string;
		ip_address: string;
		location: string;
		last_active: string;
		created_at: string;
		is_current: boolean;
	}

	let sessions = $state<Session[]>([]);
	let loading = $state(true);
	let revoking = $state<string | null>(null);

	onMount(() => {
		loadSessions();
	});

	async function loadSessions() {
		loading = true;
		// Simulate API call
		await new Promise(resolve => setTimeout(resolve, 500));

		sessions = [
			{
				id: '1',
				device: 'Desktop',
				browser: 'Chrome 120',
				ip_address: '192.168.1.100',
				location: 'San Francisco, CA',
				last_active: new Date().toISOString(),
				created_at: new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString(),
				is_current: true
			},
			{
				id: '2',
				device: 'MacBook Pro',
				browser: 'Safari 17',
				ip_address: '10.0.0.45',
				location: 'San Francisco, CA',
				last_active: new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString(),
				created_at: new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString(),
				is_current: false
			},
			{
				id: '3',
				device: 'iPhone 15',
				browser: 'Safari Mobile',
				ip_address: '172.16.0.12',
				location: 'Oakland, CA',
				last_active: new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString(),
				created_at: new Date(Date.now() - 5 * 24 * 60 * 60 * 1000).toISOString(),
				is_current: false
			}
		];
		loading = false;
	}

	async function revokeSession(session: Session) {
		if (session.is_current) {
			if (!confirm('This will log you out of your current session. Continue?')) {
				return;
			}
		} else {
			if (!confirm(`Revoke session from ${session.device}?`)) {
				return;
			}
		}

		revoking = session.id;
		try {
			await new Promise(resolve => setTimeout(resolve, 500));
			sessions = sessions.filter(s => s.id !== session.id);
			toast.success('Session revoked');
		} catch {
			toast.error('Failed to revoke session');
		} finally {
			revoking = null;
		}
	}

	async function revokeAllOther() {
		if (!confirm('Revoke all other sessions? This cannot be undone.')) {
			return;
		}

		try {
			await new Promise(resolve => setTimeout(resolve, 500));
			sessions = sessions.filter(s => s.is_current);
			toast.success('All other sessions revoked');
		} catch {
			toast.error('Failed to revoke sessions');
		}
	}

	function formatDate(dateStr: string): string {
		return new Date(dateStr).toLocaleString('en-US', {
			month: 'short',
			day: 'numeric',
			year: 'numeric',
			hour: '2-digit',
			minute: '2-digit'
		});
	}

	function formatLastActive(dateStr: string): string {
		const diff = Date.now() - new Date(dateStr).getTime();
		const minutes = Math.floor(diff / 60000);
		const hours = Math.floor(diff / 3600000);
		const days = Math.floor(diff / 86400000);

		if (minutes < 1) return 'Just now';
		if (minutes < 60) return `${minutes}m ago`;
		if (hours < 24) return `${hours}h ago`;
		return `${days}d ago`;
	}

	function getDeviceIcon(device: string): string {
		const lower = device.toLowerCase();
		if (lower.includes('iphone') || lower.includes('android') || lower.includes('mobile')) {
			return '📱';
		}
		if (lower.includes('ipad') || lower.includes('tablet')) {
			return '📟';
		}
		return '💻';
	}
</script>

<div class="max-w-4xl">
	<header class="flex justify-between items-start mb-8 flex-wrap gap-4">
		<div>
			<h1 class="text-2xl font-bold mb-1">Active Sessions</h1>
			<p class="text-[--color-text-secondary]">Manage your active login sessions</p>
		</div>
		{#if sessions.filter(s => !s.is_current).length > 0}
			<button class="btn btn-secondary" onclick={revokeAllOther}>
				Revoke All Other Sessions
			</button>
		{/if}
	</header>

	{#if loading}
		<div class="space-y-4">
			{#each Array(3) as _}
				<Skeleton height="6rem" />
			{/each}
		</div>
	{:else if sessions.length === 0}
		<div class="card text-center py-12">
			<p class="text-[--color-text-muted]">No active sessions</p>
		</div>
	{:else}
		<div class="space-y-4">
			{#each sessions as session}
				<div class="card {session.is_current ? 'ring-2 ring-[--color-primary]' : ''}">
					<div class="flex items-start justify-between gap-4">
						<div class="flex items-start gap-4">
							<div class="w-12 h-12 bg-[--color-bg-tertiary] rounded-lg flex items-center justify-center text-2xl">
								{getDeviceIcon(session.device)}
							</div>
							<div>
								<div class="flex items-center gap-2 mb-1">
									<span class="font-medium">{session.device}</span>
									{#if session.is_current}
										<span class="badge badge-success">Current Session</span>
									{/if}
								</div>
								<div class="text-sm text-[--color-text-secondary] mb-2">
									{session.browser}
								</div>
								<div class="flex flex-wrap gap-4 text-xs text-[--color-text-muted]">
									<span title="IP Address">🌐 {session.ip_address}</span>
									<span title="Location">📍 {session.location}</span>
									<span title="Last Active">🕒 {formatLastActive(session.last_active)}</span>
								</div>
							</div>
						</div>
						<div class="flex flex-col items-end gap-2">
							<button
								class="btn btn-sm {session.is_current ? 'btn-ghost text-[--color-error]' : 'btn-secondary'}"
								onclick={() => revokeSession(session)}
								disabled={revoking === session.id}
							>
								{revoking === session.id ? 'Revoking...' : session.is_current ? 'Logout' : 'Revoke'}
							</button>
							<span class="text-xs text-[--color-text-muted]">
								Started {formatDate(session.created_at)}
							</span>
						</div>
					</div>
				</div>
			{/each}
		</div>

		<!-- Security Tips -->
		<div class="card mt-6 bg-[--color-bg-tertiary]">
			<h3 class="text-sm font-semibold text-[--color-text-secondary] mb-3">Security Tips</h3>
			<ul class="text-sm text-[--color-text-muted] space-y-2">
				<li>• Review your active sessions regularly and revoke any you don't recognize</li>
				<li>• If you see unfamiliar devices or locations, change your password immediately</li>
				<li>• Use "Revoke All Other Sessions" after changing your password</li>
				<li>• Enable two-factor authentication for additional security</li>
			</ul>
		</div>
	{/if}
</div>
