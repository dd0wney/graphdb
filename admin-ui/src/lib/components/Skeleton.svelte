<script lang="ts">
	interface Props {
		class?: string;
		width?: string;
		height?: string;
		rounded?: 'sm' | 'md' | 'lg' | 'full';
	}

	let {
		class: className = '',
		width = '100%',
		height = '1rem',
		rounded = 'md'
	}: Props = $props();

	const roundedClass = {
		sm: 'rounded-sm',
		md: 'rounded',
		lg: 'rounded-lg',
		full: 'rounded-full'
	}[rounded];
</script>

<div
	class="animate-pulse bg-[--color-bg-tertiary] {roundedClass} {className}"
	style="width: {width}; height: {height}"
></div>
