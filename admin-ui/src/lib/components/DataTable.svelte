<script lang="ts" generics="T extends Record<string, any>">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';
	import Skeleton from './Skeleton.svelte';

	interface Column<T> {
		key: keyof T | string;
		label: string;
		sortable?: boolean;
		width?: string;
		align?: 'left' | 'center' | 'right';
		render?: (row: T) => string | number;
	}

	interface Props {
		data: T[];
		columns: Column<T>[];
		loading?: boolean;
		sortable?: boolean;
		sortKey?: string;
		sortDirection?: 'asc' | 'desc';
		emptyMessage?: string;
		emptyIcon?: string;
		onRowClick?: (row: T) => void;
		onSort?: (key: string, direction: 'asc' | 'desc') => void;
		rowActions?: Snippet<[T]>;
		emptyAction?: Snippet;
		cellSlots?: Record<string, Snippet<[T]>>;
	}

	let {
		data,
		columns,
		loading = false,
		sortable = false,
		sortKey = '',
		sortDirection = 'asc',
		emptyMessage = 'No data found',
		emptyIcon = 'circle-stack',
		onRowClick,
		onSort,
		rowActions,
		emptyAction,
		cellSlots = {}
	}: Props = $props();

	let currentSortKey = $state(sortKey);
	let currentSortDirection = $state<'asc' | 'desc'>(sortDirection);

	function handleSort(key: string) {
		if (!sortable) return;

		const column = columns.find(c => c.key === key);
		if (!column?.sortable) return;

		if (currentSortKey === key) {
			currentSortDirection = currentSortDirection === 'asc' ? 'desc' : 'asc';
		} else {
			currentSortKey = key;
			currentSortDirection = 'asc';
		}

		onSort?.(currentSortKey, currentSortDirection);
	}

	function getCellValue(row: T, column: Column<T>): string | number {
		if (column.render) {
			return column.render(row);
		}
		const value = row[column.key as keyof T];
		return value !== undefined && value !== null ? String(value) : '';
	}

	function getAlignClass(align?: 'left' | 'center' | 'right'): string {
		switch (align) {
			case 'center': return 'text-center';
			case 'right': return 'text-right';
			default: return 'text-left';
		}
	}

	// Client-side sorting if no onSort handler
	const sortedData = $derived(() => {
		if (!sortable || !currentSortKey || onSort) {
			return data;
		}

		return [...data].sort((a, b) => {
			const aVal = a[currentSortKey as keyof T];
			const bVal = b[currentSortKey as keyof T];

			if (aVal === bVal) return 0;
			if (aVal === null || aVal === undefined) return 1;
			if (bVal === null || bVal === undefined) return -1;

			const comparison = aVal < bVal ? -1 : 1;
			return currentSortDirection === 'asc' ? comparison : -comparison;
		});
	});

	const displayData = $derived(onSort ? data : sortedData());
</script>

<div class="w-full">
	{#if loading}
		<!-- Loading skeleton -->
		<div class="space-y-3">
			{#each Array(5) as _}
				<Skeleton height="3rem" />
			{/each}
		</div>
	{:else if data.length === 0}
		<!-- Empty state -->
		<div class="flex flex-col items-center justify-center py-12 text-[--color-text-muted]">
			<Icon name={emptyIcon as any} size={48} class="mb-4 opacity-50" />
			<p class="text-lg mb-2">{emptyMessage}</p>
			{#if emptyAction}
				<div class="mt-4">
					{@render emptyAction()}
				</div>
			{/if}
		</div>
	{:else}
		<!-- Table -->
		<div class="overflow-x-auto">
			<table class="w-full text-sm">
				<thead>
					<tr class="border-b border-[--color-border]">
						{#each columns as column}
							<th
								class="py-3 px-4 font-semibold text-[--color-text-secondary] {getAlignClass(column.align)} {column.sortable && sortable ? 'cursor-pointer hover:text-[--color-text] select-none' : ''}"
								style={column.width ? `width: ${column.width}` : ''}
								onclick={() => column.sortable && handleSort(String(column.key))}
							>
								<div class="flex items-center gap-1 {column.align === 'right' ? 'justify-end' : column.align === 'center' ? 'justify-center' : ''}">
									<span>{column.label}</span>
									{#if column.sortable && sortable}
										<span class="inline-flex flex-col text-[10px] leading-none opacity-50">
											{#if currentSortKey === column.key}
												<Icon
													name={currentSortDirection === 'asc' ? 'chevron-up' : 'chevron-down'}
													size={14}
													class="opacity-100"
												/>
											{:else}
												<Icon name="chevron-up" size={14} class="opacity-30" />
											{/if}
										</span>
									{/if}
								</div>
							</th>
						{/each}
						{#if rowActions}
							<th class="py-3 px-4 w-20"></th>
						{/if}
					</tr>
				</thead>
				<tbody>
					{#each displayData as row, index}
						<tr
							class="border-b border-[--color-border] last:border-b-0 transition-colors {onRowClick ? 'cursor-pointer hover:bg-[--color-bg-tertiary]' : ''}"
							onclick={() => onRowClick?.(row)}
						>
							{#each columns as column}
								<td class="py-3 px-4 {getAlignClass(column.align)}">
									{#if cellSlots[String(column.key)]}
										{@render cellSlots[String(column.key)](row)}
									{:else}
										{getCellValue(row, column)}
									{/if}
								</td>
							{/each}
							{#if rowActions}
								<td class="py-3 px-4 text-right" onclick={(e) => e.stopPropagation()}>
									{@render rowActions(row)}
								</td>
							{/if}
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>
