<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { auth, isAuthenticated, isAdmin } from '$lib/stores/auth';
	import { theme } from '$lib/stores/theme';
	import Toast from '$lib/components/Toast.svelte';
	import CommandPalette from '$lib/components/CommandPalette.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import favicon from '$lib/assets/favicon.svg';
	import '../app.css';

	let { children } = $props();
	let commandPaletteOpen = $state(false);
	let sidebarOpen = $state(false);

	onMount(() => {
		auth.init();
	});

	// Icon names from Heroicons
	const navItems = [
		{ href: '/', label: 'Dashboard', icon: 'squares-2x2' as const },
		{ href: '/explorer', label: 'Explorer', icon: 'circle-stack' as const },
		{ href: '/schema', label: 'Schema', icon: 'rectangle-stack' as const },
		{ href: '/metrics', label: 'Metrics', icon: 'chart-bar' as const },
		{ href: '/apikeys', label: 'API Keys', icon: 'key' as const },
		{ href: '/cluster', label: 'Cluster', icon: 'server-stack' as const },
		{ href: '/backups', label: 'Backups', icon: 'archive-box' as const },
		{ href: '/audit', label: 'Audit Logs', icon: 'clipboard-document-list' as const, adminOnly: true },
		{ href: '/users', label: 'Users', icon: 'users' as const, adminOnly: true },
		{ href: '/sessions', label: 'Sessions', icon: 'shield-check' as const },
		{ href: '/settings', label: 'Settings', icon: 'cog-6-tooth' as const }
	];

	function isActive(href: string): boolean {
		if (href === '/') return $page.url.pathname === '/';
		return $page.url.pathname.startsWith(href);
	}

	async function handleLogout() {
		await auth.logout();
		goto('/login');
	}

	function toggleTheme() {
		theme.toggle();
	}
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<title>GraphDB Admin</title>
</svelte:head>

<!-- Global Components -->
<Toast />
<CommandPalette bind:open={commandPaletteOpen} />

{#if $auth.loading}
	<div class="flex flex-col items-center justify-center h-screen gap-4 text-[--color-text-secondary]">
		<div class="spinner"></div>
		<p>Loading...</p>
	</div>
{:else if !$isAuthenticated && $page.url.pathname !== '/login'}
	<script>
		import { goto } from '$app/navigation';
		goto('/login');
	</script>
{:else if $page.url.pathname === '/login'}
	{@render children()}
{:else}
	<div class="flex min-h-screen" class:sidebar-open={sidebarOpen}>
		<!-- Mobile Header -->
		<header class="md:hidden fixed top-0 left-0 right-0 h-14 bg-[--color-bg-secondary] border-b border-[--color-border] flex items-center justify-between px-4 z-40">
			<button
				class="btn btn-ghost btn-icon"
				onclick={() => (sidebarOpen = !sidebarOpen)}
				aria-label={sidebarOpen ? 'Close navigation menu' : 'Open navigation menu'}
				aria-expanded={sidebarOpen}
				aria-controls="sidebar-nav"
			>
				<Icon name="bars-3" size={24} />
			</button>
			<span class="font-bold">GraphDB</span>
			<button
				class="btn btn-ghost btn-icon"
				onclick={() => (commandPaletteOpen = true)}
				aria-label="Open command palette"
			>
				<Icon name="magnifying-glass" size={24} />
			</button>
		</header>

		<!-- Sidebar Overlay (mobile) -->
		{#if sidebarOpen}
			<div
				class="md:hidden fixed inset-0 bg-black/50 z-40"
				onclick={() => (sidebarOpen = false)}
				onkeydown={(e) => e.key === 'Escape' && (sidebarOpen = false)}
				role="button"
				tabindex="-1"
			></div>
		{/if}

		<!-- Sidebar -->
		<aside
			id="sidebar-nav"
			class="sidebar w-64 bg-[--color-bg-secondary] border-r border-[--color-border] flex flex-col fixed h-screen z-50 md:translate-x-0"
			class:translate-x-0={sidebarOpen}
			role="navigation"
			aria-label="Main navigation"
		>
			<!-- Logo -->
			<div class="p-6 border-b border-[--color-border]">
				<div class="flex items-center justify-between">
					<div class="flex items-center gap-3 text-[--color-text] font-bold text-xl">
						<svg width="32" height="32" viewBox="0 0 32 32" fill="none" class="text-[--color-primary]" aria-hidden="true">
							<circle cx="16" cy="16" r="14" stroke="currentColor" stroke-width="2" />
							<circle cx="16" cy="10" r="3" fill="currentColor" />
							<circle cx="10" cy="20" r="3" fill="currentColor" />
							<circle cx="22" cy="20" r="3" fill="currentColor" />
							<line x1="16" y1="13" x2="10" y2="17" stroke="currentColor" stroke-width="2" />
							<line x1="16" y1="13" x2="22" y2="17" stroke="currentColor" stroke-width="2" />
							<line x1="10" y1="20" x2="22" y2="20" stroke="currentColor" stroke-width="2" />
						</svg>
						<span>GraphDB</span>
					</div>
					<button
						class="md:hidden btn btn-ghost btn-icon"
						onclick={() => (sidebarOpen = false)}
						aria-label="Close navigation menu"
					>
						<Icon name="x-mark" size={20} />
					</button>
				</div>
			</div>

			<!-- Search trigger -->
			<div class="px-3 py-2">
				<button
					class="w-full flex items-center gap-2 px-3 py-2 text-sm text-[--color-text-muted] bg-[--color-bg] border border-[--color-border] rounded-lg hover:border-[--color-text-muted] transition-colors"
					onclick={() => (commandPaletteOpen = true)}
					aria-label="Open command palette (keyboard shortcut: Cmd+K)"
				>
					<Icon name="magnifying-glass" size={16} />
					<span class="flex-1 text-left">Search...</span>
					<kbd class="hidden sm:inline">⌘K</kbd>
				</button>
			</div>

			<!-- Navigation -->
			<nav class="flex-1 p-3 overflow-y-auto" aria-label="Main">
				<ul class="list-none m-0 p-0">
					{#each navItems as item}
						{#if !item.adminOnly || $isAdmin}
							<li>
								<a
									href={item.href}
									class="flex items-center gap-3 px-4 py-2.5 rounded-lg mb-0.5 transition-all duration-150
										{isActive(item.href)
											? 'bg-[--color-primary] text-white'
											: 'text-[--color-text-secondary] hover:bg-[--color-bg-tertiary] hover:text-[--color-text]'}"
									onclick={() => (sidebarOpen = false)}
									aria-current={isActive(item.href) ? 'page' : undefined}
								>
									<Icon name={item.icon} size={20} />
									<span>{item.label}</span>
								</a>
							</li>
						{/if}
					{/each}
				</ul>
			</nav>

			<!-- Theme Toggle & User Footer -->
			<div class="border-t border-[--color-border]">
				<!-- Theme toggle -->
				<div class="px-4 py-3 flex items-center justify-between border-b border-[--color-border]">
					<span class="text-sm text-[--color-text-secondary]" id="theme-label">Theme</span>
					<button
						class="toggle {$theme === 'light' ? '' : 'active'}"
						onclick={toggleTheme}
						aria-label={$theme === 'light' ? 'Switch to dark theme' : 'Switch to light theme'}
						aria-labelledby="theme-label"
						role="switch"
						aria-checked={$theme === 'dark'}
					>
						<span class="toggle-knob"></span>
					</button>
				</div>

				<!-- User -->
				<div class="p-4 flex items-center gap-3">
					<div class="flex-1 flex items-center gap-3">
						<div class="w-9 h-9 bg-[--color-primary] rounded-full flex items-center justify-center font-semibold text-white">
							{$auth.user?.username?.charAt(0).toUpperCase() || '?'}
						</div>
						<div class="flex-1 min-w-0">
							<div class="font-medium truncate">{$auth.user?.username || 'Unknown'}</div>
							<div class="text-xs text-[--color-text-muted] capitalize">{$auth.user?.role || 'user'}</div>
						</div>
					</div>
					<button
						class="btn btn-ghost btn-icon"
						onclick={handleLogout}
						aria-label="Sign out"
					>
						<Icon name="arrow-right-on-rectangle" size={20} />
					</button>
				</div>
			</div>
		</aside>

		<!-- Main Content -->
		<main class="flex-1 md:ml-64 p-4 md:p-8 min-h-screen pt-18 md:pt-8">
			{@render children()}
		</main>
	</div>
{/if}
