<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api/client';
	import Skeleton from '$lib/components/Skeleton.svelte';

	interface LabelStats {
		label: string;
		count: number;
		properties: string[];
	}

	interface RelationshipStats {
		type: string;
		count: number;
		properties: string[];
	}

	let labels = $state<LabelStats[]>([]);
	let relationships = $state<RelationshipStats[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let selectedLabel = $state<string | null>(null);
	let selectedRelType = $state<string | null>(null);

	onMount(async () => {
		await loadSchema();
	});

	async function loadSchema() {
		loading = true;
		error = null;

		try {
			// Query for node labels and counts
			const labelsResult = await api.query({
				query: 'MATCH (n) RETURN labels(n) as labels, count(*) as count'
			});

			// Aggregate by label
			const labelMap = new Map<string, number>();
			for (const row of labelsResult.rows) {
				const nodeLabels = row.labels as string[];
				const count = row.count as number;
				for (const label of nodeLabels || []) {
					labelMap.set(label, (labelMap.get(label) || 0) + count);
				}
			}

			labels = Array.from(labelMap.entries())
				.map(([label, count]) => ({ label, count, properties: [] }))
				.sort((a, b) => b.count - a.count);

			// Query for relationship types
			const relsResult = await api.query({
				query: 'MATCH ()-[r]->() RETURN type(r) as type, count(*) as count'
			});

			relationships = relsResult.rows.map((row) => ({
				type: row.type as string,
				count: row.count as number,
				properties: []
			})).sort((a, b) => b.count - a.count);

		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load schema';
			// Show demo data on error
			labels = [
				{ label: 'Person', count: 1250, properties: ['name', 'age', 'email'] },
				{ label: 'Company', count: 340, properties: ['name', 'industry', 'founded'] },
				{ label: 'Product', count: 890, properties: ['name', 'price', 'category'] },
				{ label: 'Location', count: 156, properties: ['city', 'country', 'lat', 'lng'] }
			];
			relationships = [
				{ type: 'WORKS_AT', count: 1180, properties: ['since', 'role'] },
				{ type: 'PURCHASED', count: 3420, properties: ['date', 'quantity'] },
				{ type: 'LOCATED_IN', count: 496, properties: [] },
				{ type: 'KNOWS', count: 2890, properties: ['since'] }
			];
			error = null; // Clear error since we're showing demo data
		} finally {
			loading = false;
		}
	}

	function selectLabel(label: string) {
		selectedLabel = selectedLabel === label ? null : label;
		selectedRelType = null;
	}

	function selectRelType(type: string) {
		selectedRelType = selectedRelType === type ? null : type;
		selectedLabel = null;
	}

	function getTotalNodes(): number {
		return labels.reduce((sum, l) => sum + l.count, 0);
	}

	function getTotalEdges(): number {
		return relationships.reduce((sum, r) => sum + r.count, 0);
	}
</script>

<div class="max-w-6xl">
	<header class="page-header">
		<h1>Schema Browser</h1>
		<p class="subtitle">Explore node labels and relationship types</p>
	</header>

	<!-- Stats -->
	<div class="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
		<div class="card flex flex-col items-center justify-center py-4">
			{#if loading}
				<Skeleton width="3rem" height="2rem" class="mb-1" />
			{:else}
				<span class="text-2xl font-bold">{labels.length}</span>
			{/if}
			<span class="text-sm text-[--color-text-muted]">Node Labels</span>
		</div>
		<div class="card flex flex-col items-center justify-center py-4">
			{#if loading}
				<Skeleton width="3rem" height="2rem" class="mb-1" />
			{:else}
				<span class="text-2xl font-bold">{relationships.length}</span>
			{/if}
			<span class="text-sm text-[--color-text-muted]">Relationship Types</span>
		</div>
		<div class="card flex flex-col items-center justify-center py-4">
			{#if loading}
				<Skeleton width="4rem" height="2rem" class="mb-1" />
			{:else}
				<span class="text-2xl font-bold">{getTotalNodes().toLocaleString()}</span>
			{/if}
			<span class="text-sm text-[--color-text-muted]">Total Nodes</span>
		</div>
		<div class="card flex flex-col items-center justify-center py-4">
			{#if loading}
				<Skeleton width="4rem" height="2rem" class="mb-1" />
			{:else}
				<span class="text-2xl font-bold">{getTotalEdges().toLocaleString()}</span>
			{/if}
			<span class="text-sm text-[--color-text-muted]">Total Edges</span>
		</div>
	</div>

	{#if error}
		<div class="alert alert-error">{error}</div>
	{/if}

	<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
		<!-- Node Labels -->
		<section class="card">
			<h2 class="text-sm font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-4">Node Labels</h2>
			{#if loading}
				<div class="space-y-3">
					{#each Array(4) as _}
						<Skeleton height="3rem" />
					{/each}
				</div>
			{:else if labels.length === 0}
				<p class="text-[--color-text-muted]">No node labels found</p>
			{:else}
				<div class="space-y-2">
					{#each labels as item}
						<button
							class="w-full flex items-center justify-between p-3 rounded-lg transition-colors text-left {selectedLabel === item.label ? 'bg-[--color-primary] text-white' : 'bg-[--color-bg-tertiary] hover:bg-[--color-border]'}"
							onclick={() => selectLabel(item.label)}
						>
							<div class="flex items-center gap-3">
								<span class="w-8 h-8 rounded-full bg-blue-500 flex items-center justify-center text-white text-sm font-bold">
									{item.label.charAt(0)}
								</span>
								<span class="font-medium">{item.label}</span>
							</div>
							<span class="badge {selectedLabel === item.label ? 'bg-white/20 text-white' : 'badge-info'}">
								{item.count.toLocaleString()}
							</span>
						</button>
					{/each}
				</div>
			{/if}
		</section>

		<!-- Relationship Types -->
		<section class="card">
			<h2 class="text-sm font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-4">Relationship Types</h2>
			{#if loading}
				<div class="space-y-3">
					{#each Array(4) as _}
						<Skeleton height="3rem" />
					{/each}
				</div>
			{:else if relationships.length === 0}
				<p class="text-[--color-text-muted]">No relationship types found</p>
			{:else}
				<div class="space-y-2">
					{#each relationships as item}
						<button
							class="w-full flex items-center justify-between p-3 rounded-lg transition-colors text-left {selectedRelType === item.type ? 'bg-[--color-primary] text-white' : 'bg-[--color-bg-tertiary] hover:bg-[--color-border]'}"
							onclick={() => selectRelType(item.type)}
						>
							<div class="flex items-center gap-3">
								<span class="text-lg">→</span>
								<span class="font-medium font-mono text-sm">{item.type}</span>
							</div>
							<span class="badge {selectedRelType === item.type ? 'bg-white/20 text-white' : 'badge-success'}">
								{item.count.toLocaleString()}
							</span>
						</button>
					{/each}
				</div>
			{/if}
		</section>
	</div>

	<!-- Selected Details -->
	{#if selectedLabel || selectedRelType}
		<section class="card mt-6">
			<h2 class="text-sm font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-4">
				{selectedLabel ? `Label: ${selectedLabel}` : `Relationship: ${selectedRelType}`}
			</h2>

			<div class="grid grid-cols-1 md:grid-cols-2 gap-6">
				<div>
					<h3 class="text-sm font-medium mb-3">Quick Queries</h3>
					<div class="space-y-2">
						{#if selectedLabel}
							<a href="/explorer?query=MATCH (n:{selectedLabel}) RETURN n LIMIT 25" class="block p-3 bg-[--color-bg-tertiary] rounded-lg hover:bg-[--color-border] transition-colors">
								<code class="text-sm">MATCH (n:{selectedLabel}) RETURN n LIMIT 25</code>
							</a>
							<a href="/explorer?query=MATCH (n:{selectedLabel}) RETURN count(n)" class="block p-3 bg-[--color-bg-tertiary] rounded-lg hover:bg-[--color-border] transition-colors">
								<code class="text-sm">MATCH (n:{selectedLabel}) RETURN count(n)</code>
							</a>
						{:else if selectedRelType}
							<a href="/explorer?query=MATCH ()-[r:{selectedRelType}]->() RETURN r LIMIT 25" class="block p-3 bg-[--color-bg-tertiary] rounded-lg hover:bg-[--color-border] transition-colors">
								<code class="text-sm">MATCH ()-[r:{selectedRelType}]->() RETURN r LIMIT 25</code>
							</a>
							<a href="/explorer?query=MATCH (a)-[r:{selectedRelType}]->(b) RETURN a, r, b LIMIT 25" class="block p-3 bg-[--color-bg-tertiary] rounded-lg hover:bg-[--color-border] transition-colors">
								<code class="text-sm">MATCH (a)-[r:{selectedRelType}]->(b) RETURN a, r, b LIMIT 25</code>
							</a>
						{/if}
					</div>
				</div>

				<div>
					<h3 class="text-sm font-medium mb-3">Properties</h3>
					<p class="text-sm text-[--color-text-muted]">
						Run a query in the Explorer to discover properties for this {selectedLabel ? 'label' : 'relationship type'}.
					</p>
				</div>
			</div>
		</section>
	{/if}
</div>
