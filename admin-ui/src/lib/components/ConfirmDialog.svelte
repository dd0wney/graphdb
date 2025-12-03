<script lang="ts">
	import Icon from './Icon.svelte';

	interface Props {
		open: boolean;
		title: string;
		message: string;
		confirmText?: string;
		cancelText?: string;
		variant?: 'danger' | 'warning' | 'default';
		loading?: boolean;
		onConfirm: () => void | Promise<void>;
		onCancel?: () => void;
	}

	let {
		open = $bindable(),
		title,
		message,
		confirmText = 'Confirm',
		cancelText = 'Cancel',
		variant = 'default',
		loading = false,
		onConfirm,
		onCancel
	}: Props = $props();

	let isProcessing = $state(false);

	const variantConfig = {
		danger: {
			icon: 'exclamation-triangle' as const,
			iconBg: 'bg-red-100 text-red-600',
			buttonClass: 'bg-red-600 hover:bg-red-700 text-white'
		},
		warning: {
			icon: 'exclamation-circle' as const,
			iconBg: 'bg-yellow-100 text-yellow-600',
			buttonClass: 'bg-yellow-600 hover:bg-yellow-700 text-white'
		},
		default: {
			icon: 'information-circle' as const,
			iconBg: 'bg-blue-100 text-blue-600',
			buttonClass: 'btn-primary'
		}
	};

	const config = $derived(variantConfig[variant]);

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && !isProcessing) {
			e.preventDefault();
			cancel();
		}
	}

	function handleBackdropClick(e: MouseEvent) {
		if (e.target === e.currentTarget && !isProcessing) {
			cancel();
		}
	}

	function cancel() {
		open = false;
		onCancel?.();
	}

	async function confirm() {
		isProcessing = true;
		try {
			await onConfirm();
			open = false;
		} catch (err) {
			// Let the caller handle errors
			throw err;
		} finally {
			isProcessing = false;
		}
	}

	// Lock body scroll when open
	$effect(() => {
		if (open) {
			document.body.style.overflow = 'hidden';
		} else {
			document.body.style.overflow = '';
		}
	});
</script>

{#if open}
	<!-- svelte-ignore a11y_interactive_supports_focus -->
	<div
		class="fixed inset-0 z-50 flex items-center justify-center p-4"
		role="alertdialog"
		aria-modal="true"
		aria-labelledby="confirm-title"
		aria-describedby="confirm-message"
		onkeydown={handleKeydown}
	>
		<!-- Backdrop -->
		<div
			class="absolute inset-0 bg-black/70 transition-opacity"
			onclick={handleBackdropClick}
			aria-hidden="true"
		></div>

		<!-- Dialog -->
		<div class="relative bg-[--color-bg-secondary] border border-[--color-border] rounded-[--radius-lg] w-full max-w-md shadow-2xl animate-in fade-in zoom-in-95 duration-200">
			<div class="p-6">
				<div class="flex items-start gap-4">
					<!-- Icon -->
					<div class="shrink-0 w-10 h-10 rounded-full {config.iconBg} flex items-center justify-center">
						<Icon name={config.icon} size={24} />
					</div>

					<!-- Content -->
					<div class="flex-1 min-w-0">
						<h3 id="confirm-title" class="text-lg font-semibold mb-2">{title}</h3>
						<p id="confirm-message" class="text-[--color-text-secondary] text-sm">{message}</p>
					</div>
				</div>
			</div>

			<!-- Actions -->
			<div class="flex justify-end gap-3 p-4 bg-[--color-bg-tertiary] border-t border-[--color-border] rounded-b-[--radius-lg]">
				<button
					type="button"
					class="btn btn-secondary"
					onclick={cancel}
					disabled={isProcessing || loading}
				>
					{cancelText}
				</button>
				<button
					type="button"
					class="btn {config.buttonClass}"
					onclick={confirm}
					disabled={isProcessing || loading}
				>
					{#if isProcessing || loading}
						<span class="inline-block w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin mr-2"></span>
					{/if}
					{confirmText}
				</button>
			</div>
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
