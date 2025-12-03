<script lang="ts">
	import { toast } from '$lib/stores/toast';
	import { fly } from 'svelte/transition';

	function getIcon(type: string): string {
		switch (type) {
			case 'success': return '✓';
			case 'error': return '✕';
			case 'warning': return '⚠';
			case 'info': return 'ℹ';
			default: return '•';
		}
	}

	function getTypeClass(type: string): string {
		switch (type) {
			case 'success': return 'bg-green-500/20 border-green-500/50 text-green-400';
			case 'error': return 'bg-red-500/20 border-red-500/50 text-red-400';
			case 'warning': return 'bg-amber-500/20 border-amber-500/50 text-amber-400';
			case 'info': return 'bg-blue-500/20 border-blue-500/50 text-blue-400';
			default: return 'bg-gray-500/20 border-gray-500/50 text-gray-400';
		}
	}
</script>

<div class="fixed bottom-4 right-4 z-[100] flex flex-col gap-2 max-w-sm">
	{#each $toast as t (t.id)}
		<div
			class="flex items-start gap-3 p-4 rounded-lg border backdrop-blur-sm shadow-lg {getTypeClass(t.type)}"
			transition:fly={{ x: 100, duration: 200 }}
		>
			<span class="text-lg font-bold">{getIcon(t.type)}</span>
			<p class="flex-1 text-sm text-[--color-text]">{t.message}</p>
			<button
				class="text-[--color-text-muted] hover:text-[--color-text] transition-colors"
				onclick={() => toast.remove(t.id)}
			>
				✕
			</button>
		</div>
	{/each}
</div>
