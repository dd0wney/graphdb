<script lang="ts">
	import { goto } from '$app/navigation';
	import { fade } from 'svelte/transition';

	interface Command {
		id: string;
		title: string;
		subtitle?: string;
		icon: string;
		action: () => void;
		keywords?: string[];
	}

	let { open = $bindable(false) }: { open: boolean } = $props();

	let search = $state('');
	let selectedIndex = $state(0);
	let inputEl: HTMLInputElement;

	const commands: Command[] = [
		{ id: 'dashboard', title: 'Go to Dashboard', icon: '⊞', action: () => goto('/'), keywords: ['home'] },
		{ id: 'explorer', title: 'Open Explorer', subtitle: 'Query the database', icon: '🗄', action: () => goto('/explorer'), keywords: ['query', 'cypher'] },
		{ id: 'apikeys', title: 'Manage API Keys', icon: '🔑', action: () => goto('/apikeys'), keywords: ['keys', 'tokens'] },
		{ id: 'cluster', title: 'View Cluster Status', icon: '🖥', action: () => goto('/cluster'), keywords: ['nodes', 'replicas'] },
		{ id: 'audit', title: 'View Audit Logs', icon: '📋', action: () => goto('/audit'), keywords: ['logs', 'security'] },
		{ id: 'users', title: 'User Management', icon: '👥', action: () => goto('/users'), keywords: ['roles', 'permissions'] },
		{ id: 'schema', title: 'Schema Browser', icon: '📐', action: () => goto('/schema'), keywords: ['labels', 'types'] },
		{ id: 'metrics', title: 'View Metrics', icon: '📊', action: () => goto('/metrics'), keywords: ['performance', 'charts'] },
		{ id: 'settings', title: 'Settings', icon: '⚙', action: () => goto('/settings'), keywords: ['config', 'preferences'] },
		{ id: 'backups', title: 'Backup Management', icon: '💾', action: () => goto('/backups'), keywords: ['restore', 'snapshot'] },
		{ id: 'theme', title: 'Toggle Theme', subtitle: 'Switch dark/light mode', icon: '🌙', action: () => { document.documentElement.classList.toggle('light-theme'); }, keywords: ['dark', 'light'] },
	];

	const filteredCommands = $derived(
		search.trim() === ''
			? commands
			: commands.filter(cmd => {
				const searchLower = search.toLowerCase();
				return cmd.title.toLowerCase().includes(searchLower) ||
					cmd.subtitle?.toLowerCase().includes(searchLower) ||
					cmd.keywords?.some(k => k.includes(searchLower));
			})
	);

	$effect(() => {
		if (open && inputEl) {
			search = '';
			selectedIndex = 0;
			setTimeout(() => inputEl?.focus(), 10);
		}
	});

	$effect(() => {
		// Reset selection when filter changes
		if (selectedIndex >= filteredCommands.length) {
			selectedIndex = 0;
		}
	});

	function handleKeydown(e: KeyboardEvent) {
		switch (e.key) {
			case 'ArrowDown':
				e.preventDefault();
				selectedIndex = Math.min(selectedIndex + 1, filteredCommands.length - 1);
				break;
			case 'ArrowUp':
				e.preventDefault();
				selectedIndex = Math.max(selectedIndex - 1, 0);
				break;
			case 'Enter':
				e.preventDefault();
				if (filteredCommands[selectedIndex]) {
					executeCommand(filteredCommands[selectedIndex]);
				}
				break;
			case 'Escape':
				open = false;
				break;
		}
	}

	function executeCommand(cmd: Command) {
		open = false;
		cmd.action();
	}
</script>

<svelte:window onkeydown={(e) => {
	if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
		e.preventDefault();
		open = !open;
	}
}} />

{#if open}
	<div
		class="fixed inset-0 bg-black/60 z-[200] flex items-start justify-center pt-[15vh]"
		transition:fade={{ duration: 100 }}
		onclick={() => (open = false)}
		onkeydown={(e) => e.key === 'Escape' && (open = false)}
		role="dialog"
		aria-modal="true"
		tabindex="-1"
	>
		<div
			class="w-full max-w-lg bg-[--color-bg-secondary] border border-[--color-border] rounded-xl shadow-2xl overflow-hidden"
			onclick={(e) => e.stopPropagation()}
			onkeydown={handleKeydown}
			role="listbox"
			tabindex="-1"
		>
			<div class="flex items-center gap-3 p-4 border-b border-[--color-border]">
				<span class="text-[--color-text-muted]">🔍</span>
				<input
					bind:this={inputEl}
					bind:value={search}
					type="text"
					placeholder="Type a command or search..."
					class="flex-1 bg-transparent border-none outline-none text-[--color-text] placeholder:text-[--color-text-muted]"
				/>
				<kbd class="px-2 py-1 bg-[--color-bg-tertiary] rounded text-xs text-[--color-text-muted]">ESC</kbd>
			</div>

			<div class="max-h-80 overflow-y-auto p-2">
				{#if filteredCommands.length === 0}
					<div class="p-4 text-center text-[--color-text-muted]">
						No commands found
					</div>
				{:else}
					{#each filteredCommands as cmd, i}
						<button
							class="w-full flex items-center gap-3 p-3 rounded-lg text-left transition-colors {i === selectedIndex ? 'bg-[--color-primary] text-white' : 'hover:bg-[--color-bg-tertiary]'}"
							onclick={() => executeCommand(cmd)}
							onmouseenter={() => (selectedIndex = i)}
						>
							<span class="text-xl">{cmd.icon}</span>
							<div class="flex-1">
								<div class="font-medium">{cmd.title}</div>
								{#if cmd.subtitle}
									<div class="text-sm opacity-70">{cmd.subtitle}</div>
								{/if}
							</div>
							{#if i === selectedIndex}
								<kbd class="px-2 py-1 bg-white/20 rounded text-xs">↵</kbd>
							{/if}
						</button>
					{/each}
				{/if}
			</div>

			<div class="flex items-center justify-between p-3 border-t border-[--color-border] text-xs text-[--color-text-muted]">
				<div class="flex gap-2">
					<span><kbd class="px-1 bg-[--color-bg-tertiary] rounded">↑↓</kbd> navigate</span>
					<span><kbd class="px-1 bg-[--color-bg-tertiary] rounded">↵</kbd> select</span>
				</div>
				<span><kbd class="px-1 bg-[--color-bg-tertiary] rounded">⌘K</kbd> toggle</span>
			</div>
		</div>
	</div>
{/if}
