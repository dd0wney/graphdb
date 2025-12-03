<script lang="ts">
	import { onMount } from 'svelte';
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';

	interface Props {
		open: boolean;
		title: string;
		size?: 'sm' | 'md' | 'lg' | 'xl';
		closable?: boolean;
		onClose: () => void;
		children: Snippet;
		footer?: Snippet;
	}

	let {
		open = $bindable(),
		title,
		size = 'md',
		closable = true,
		onClose,
		children,
		footer
	}: Props = $props();

	let dialogEl: HTMLDivElement | null = null;
	let previousActiveElement: Element | null = null;

	const sizeClasses = {
		sm: 'max-w-sm',
		md: 'max-w-md',
		lg: 'max-w-lg',
		xl: 'max-w-xl'
	};

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && closable) {
			e.preventDefault();
			close();
		}
	}

	function handleBackdropClick(e: MouseEvent) {
		if (e.target === e.currentTarget && closable) {
			close();
		}
	}

	function close() {
		open = false;
		onClose?.();
	}

	// Focus trap and body scroll lock
	$effect(() => {
		if (open) {
			// Store current focus
			previousActiveElement = document.activeElement;

			// Lock body scroll
			document.body.style.overflow = 'hidden';

			// Focus the dialog
			setTimeout(() => {
				const firstFocusable = dialogEl?.querySelector<HTMLElement>(
					'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
				);
				firstFocusable?.focus();
			}, 0);
		} else {
			// Restore body scroll
			document.body.style.overflow = '';

			// Restore focus
			if (previousActiveElement instanceof HTMLElement) {
				previousActiveElement.focus();
			}
		}
	});
</script>

{#if open}
	<!-- svelte-ignore a11y_interactive_supports_focus -->
	<div
		class="fixed inset-0 z-50 flex items-center justify-center p-4"
		role="dialog"
		aria-modal="true"
		aria-labelledby="modal-title"
		onkeydown={handleKeydown}
	>
		<!-- Backdrop -->
		<div
			class="absolute inset-0 bg-black/70 transition-opacity"
			onclick={handleBackdropClick}
			aria-hidden="true"
		></div>

		<!-- Dialog -->
		<div
			bind:this={dialogEl}
			class="relative bg-[--color-bg-secondary] border border-[--color-border] rounded-[--radius-lg] w-full {sizeClasses[size]} max-h-[90vh] overflow-hidden flex flex-col shadow-2xl animate-in fade-in zoom-in-95 duration-200"
		>
			<!-- Header -->
			<div class="flex justify-between items-center p-5 border-b border-[--color-border] shrink-0">
				<h2 id="modal-title" class="text-lg font-semibold">{title}</h2>
				{#if closable}
					<button
						type="button"
						class="btn btn-ghost btn-icon"
						onclick={close}
						aria-label="Close modal"
					>
						<Icon name="x-mark" size={20} />
					</button>
				{/if}
			</div>

			<!-- Body -->
			<div class="p-5 overflow-y-auto flex-1">
				{@render children()}
			</div>

			<!-- Footer (optional) -->
			{#if footer}
				<div class="flex justify-end gap-3 p-5 border-t border-[--color-border] bg-[--color-bg-tertiary] shrink-0">
					{@render footer()}
				</div>
			{/if}
		</div>
	</div>
{/if}

<style>
	@keyframes fade-in {
		from { opacity: 0; }
		to { opacity: 1; }
	}

	@keyframes zoom-in-95 {
		from { transform: scale(0.95); }
		to { transform: scale(1); }
	}

	.animate-in {
		animation: fade-in 0.2s ease-out, zoom-in-95 0.2s ease-out;
	}
</style>
