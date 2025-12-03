<script lang="ts">
	import { api } from '$lib/api/client';
	import { toast } from '$lib/stores/toast';
	import GraphVisualization from '$lib/components/GraphVisualization.svelte';
	import QueryEditor from '$lib/components/QueryEditor.svelte';
	import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
	import type { QueryResponse } from '$lib/api/types';

	let query = $state('MATCH (n)-[r]->(m) RETURN n, r, m LIMIT 50');
	let results = $state<QueryResponse | null>(null);
	let loading = $state(false);
	let error = $state<string | null>(null);
	let history = $state<string[]>([]);
	let viewMode = $state<'table' | 'graph'>('graph');
	let graphComponent: GraphVisualization;

	// Extracted graph data for visualization
	let graphNodes = $state<any[]>([]);
	let graphEdges = $state<any[]>([]);

	// Node/Edge editing
	let showNodeModal = $state(false);
	let showEdgeModal = $state(false);
	let editingNode = $state<any | null>(null);
	let editingEdge = $state<any | null>(null);
	let nodeForm = $state({ labels: '', properties: '{}' });
	let edgeForm = $state({ type: '', fromId: '', toId: '', properties: '{}' });
	let savingNode = $state(false);
	let savingEdge = $state(false);

	// Confirmation dialogs
	let showDeleteNodeConfirm = $state(false);
	let showDeleteEdgeConfirm = $state(false);

	async function executeQuery() {
		if (!query.trim()) return;

		loading = true;
		error = null;
		results = null;
		graphNodes = [];
		graphEdges = [];

		try {
			results = await api.query({ query: query.trim() });

			// Extract nodes and edges from results for visualization
			extractGraphData(results);

			// Add to history if not duplicate
			if (!history.includes(query.trim())) {
				history = [query.trim(), ...history.slice(0, 9)];
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Query execution failed';
		} finally {
			loading = false;
		}
	}

	function extractGraphData(response: QueryResponse) {
		const nodeMap = new Map<string | number, any>();
		const edgeMap = new Map<string | number, any>();

		for (const row of response.rows) {
			for (const value of Object.values(row)) {
				if (isNode(value)) {
					const node = value as any;
					if (!nodeMap.has(node.id)) {
						nodeMap.set(node.id, {
							id: node.id,
							labels: node.labels || [],
							properties: node.properties || {}
						});
					}
				} else if (isEdge(value)) {
					const edge = value as any;
					if (!edgeMap.has(edge.id)) {
						edgeMap.set(edge.id, {
							id: edge.id,
							source: edge.from_node_id || edge.source,
							target: edge.to_node_id || edge.target,
							type: edge.type || 'RELATED',
							properties: edge.properties || {}
						});
					}
				}
			}
		}

		graphNodes = Array.from(nodeMap.values());
		graphEdges = Array.from(edgeMap.values());

		// Auto-switch to graph view if we have graph data
		if (graphNodes.length > 0 && graphEdges.length > 0) {
			viewMode = 'graph';
		}
	}

	function isNode(value: unknown): boolean {
		if (!value || typeof value !== 'object') return false;
		const v = value as Record<string, unknown>;
		return 'id' in v && ('labels' in v || 'properties' in v) && !('type' in v && ('from_node_id' in v || 'source' in v));
	}

	function isEdge(value: unknown): boolean {
		if (!value || typeof value !== 'object') return false;
		const v = value as Record<string, unknown>;
		return 'type' in v && ('from_node_id' in v || 'to_node_id' in v || 'source' in v || 'target' in v);
	}

	function loadFromHistory(q: string) {
		query = q;
	}

	function clearHistory() {
		history = [];
	}

	function handleNodeClick(node: any) {
		editingNode = node;
		nodeForm = {
			labels: (node.labels || []).join(', '),
			properties: JSON.stringify(node.properties || {}, null, 2)
		};
		showNodeModal = true;
	}

	function handleEdgeClick(edge: any) {
		editingEdge = edge;
		edgeForm = {
			type: edge.type || '',
			fromId: edge.source?.toString() || '',
			toId: edge.target?.toString() || '',
			properties: JSON.stringify(edge.properties || {}, null, 2)
		};
		showEdgeModal = true;
	}

	// Export functionality
	function exportToJSON() {
		if (!results) return;

		const data = {
			query: query,
			columns: results.columns,
			rows: results.rows,
			count: results.count,
			time: results.time,
			exported_at: new Date().toISOString()
		};

		downloadFile(JSON.stringify(data, null, 2), 'query-results.json', 'application/json');
		toast.success('Exported to JSON');
	}

	function exportToCSV() {
		if (!results || results.rows.length === 0) return;

		const columns = results.columns;
		const headers = columns.join(',');
		const rows = results.rows.map(row =>
			columns.map(col => {
				const val = row[col];
				if (val === null || val === undefined) return '';
				if (typeof val === 'object') return `"${JSON.stringify(val).replace(/"/g, '""')}"`;
				if (typeof val === 'string') return `"${val.replace(/"/g, '""')}"`;
				return val;
			}).join(',')
		);

		const csv = [headers, ...rows].join('\n');
		downloadFile(csv, 'query-results.csv', 'text/csv');
		toast.success('Exported to CSV');
	}

	function downloadFile(content: string, filename: string, mimeType: string) {
		const blob = new Blob([content], { type: mimeType });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = filename;
		a.click();
		URL.revokeObjectURL(url);
	}

	// Node CRUD operations
	async function saveNode() {
		savingNode = true;
		try {
			const labels = nodeForm.labels.split(',').map(l => l.trim()).filter(Boolean);
			const properties = JSON.parse(nodeForm.properties);

			if (editingNode) {
				// Update existing node
				await api.updateNode(editingNode.id, { labels, properties });
				toast.success(`Node ${editingNode.id} updated successfully`);
			} else {
				// Create new node
				const newNode = await api.createNode({ labels, properties });
				toast.success(`Node ${newNode.id} created successfully`);
			}

			showNodeModal = false;
			editingNode = null;
			// Re-execute query to refresh
			await executeQuery();
		} catch (err) {
			if (err instanceof SyntaxError) {
				toast.error('Invalid JSON in properties');
			} else {
				toast.error(err instanceof Error ? err.message : 'Failed to save node');
			}
		} finally {
			savingNode = false;
		}
	}

	async function deleteNode() {
		if (!editingNode) return;

		try {
			await api.deleteNode(editingNode.id);
			toast.success(`Node ${editingNode.id} deleted`);
			showNodeModal = false;
			showDeleteNodeConfirm = false;
			editingNode = null;
			await executeQuery();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Failed to delete node');
		}
	}

	function confirmDeleteNode() {
		showDeleteNodeConfirm = true;
	}

	// Edge CRUD operations
	async function saveEdge() {
		savingEdge = true;
		try {
			const properties = JSON.parse(edgeForm.properties);

			if (editingEdge) {
				await api.updateEdge(editingEdge.id, {
					type: edgeForm.type,
					properties
				});
				toast.success('Edge updated successfully');
			} else {
				const fromId = parseInt(edgeForm.fromId);
				const toId = parseInt(edgeForm.toId);
				if (isNaN(fromId) || isNaN(toId)) {
					throw new Error('Node IDs must be valid numbers');
				}
				await api.createEdge({
					from_node_id: fromId,
					to_node_id: toId,
					type: edgeForm.type,
					properties
				});
				toast.success('Edge created successfully');
			}

			showEdgeModal = false;
			editingEdge = null;
			await executeQuery();
		} catch (err) {
			if (err instanceof SyntaxError) {
				toast.error('Invalid JSON in properties');
			} else {
				toast.error(err instanceof Error ? err.message : 'Failed to save edge');
			}
		} finally {
			savingEdge = false;
		}
	}

	async function deleteEdge() {
		if (!editingEdge) return;

		try {
			await api.deleteEdge(editingEdge.id);
			toast.success('Edge deleted');
			showEdgeModal = false;
			showDeleteEdgeConfirm = false;
			editingEdge = null;
			await executeQuery();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : 'Failed to delete edge');
		}
	}

	function confirmDeleteEdge() {
		showDeleteEdgeConfirm = true;
	}

	function openCreateNode() {
		editingNode = null;
		nodeForm = { labels: '', properties: '{}' };
		showNodeModal = true;
	}

	function openCreateEdge() {
		editingEdge = null;
		edgeForm = { type: '', fromId: '', toId: '', properties: '{}' };
		showEdgeModal = true;
	}

	const exampleQueries = [
		{ label: 'Graph with relationships', query: 'MATCH (n)-[r]->(m) RETURN n, r, m LIMIT 50' },
		{ label: 'All nodes', query: 'MATCH (n) RETURN n LIMIT 100' },
		{ label: 'Paths from node', query: 'MATCH path = (n)-[*1..3]->(m) WHERE id(n) = 1 RETURN path LIMIT 25' },
		{ label: 'Node neighbors', query: 'MATCH (n)-[r]-(neighbor) WHERE id(n) = 1 RETURN n, r, neighbor' }
	];
</script>

<div class="w-full">
	<header class="flex justify-between items-start mb-6 flex-wrap gap-4">
		<div>
			<h1 class="text-2xl font-bold mb-1">Database Explorer</h1>
			<p class="text-[--color-text-secondary]">Query and visualize your graph data</p>
		</div>
		<div class="flex gap-2">
			<button class="btn btn-secondary" onclick={openCreateNode}>
				+ Node
			</button>
			<button class="btn btn-secondary" onclick={openCreateEdge}>
				+ Edge
			</button>
		</div>
	</header>

	<div class="grid grid-cols-1 lg:grid-cols-[320px_1fr] gap-6 items-start">
		<div class="flex flex-col gap-4">
			<div class="card">
				<div class="flex justify-between items-center mb-3">
					<h2 class="text-sm font-semibold text-[--color-text-secondary]">Query Editor</h2>
					<span class="text-xs text-[--color-text-muted]">Ctrl+Enter to execute</span>
				</div>
				<QueryEditor
					bind:value={query}
					placeholder="Enter your Cypher query..."
					onExecute={executeQuery}
				/>
				<div class="flex gap-2 mt-3">
					<button class="btn btn-primary" onclick={executeQuery} disabled={loading}>
						{loading ? 'Executing...' : 'Execute Query'}
					</button>
					<button class="btn btn-secondary" onclick={() => (query = '')}>
						Clear
					</button>
				</div>
			</div>

			<!-- Quick Examples -->
			<div class="card">
				<h3 class="text-sm font-semibold text-[--color-text-secondary] mb-3">Quick Examples</h3>
				<div class="flex flex-col gap-2">
					{#each exampleQueries as example}
						<button
							class="w-full p-2 px-3 bg-[--color-bg-tertiary] border-none rounded-[--radius] text-[--color-text] text-left text-sm cursor-pointer transition-colors hover:bg-[--color-border]"
							onclick={() => (query = example.query)}
						>
							{example.label}
						</button>
					{/each}
				</div>
			</div>

			<!-- Query History -->
			<div class="card">
				<div class="flex justify-between items-center mb-3">
					<h3 class="text-sm font-semibold text-[--color-text-secondary]">History</h3>
					{#if history.length > 0}
						<button class="btn btn-sm btn-secondary" onclick={clearHistory}>
							Clear
						</button>
					{/if}
				</div>
				{#if history.length > 0}
					<div class="flex flex-col gap-2">
						{#each history as h, i}
							<button class="flex items-start gap-2 w-full p-2 bg-[--color-bg-tertiary] border-none rounded-[--radius] text-left cursor-pointer transition-colors hover:bg-[--color-border]" onclick={() => loadFromHistory(h)}>
								<span class="shrink-0 w-6 h-6 bg-[--color-bg] rounded-full flex items-center justify-center text-xs text-[--color-text-muted]">{i + 1}</span>
								<code class="text-xs text-[--color-text-secondary] overflow-hidden line-clamp-2">{h}</code>
							</button>
						{/each}
					</div>
				{:else}
					<p class="text-sm text-[--color-text-muted]">No query history</p>
				{/if}
			</div>
		</div>

		<div class="min-h-[500px]">
			{#if error}
				<div class="alert alert-error">{error}</div>
			{/if}

			{#if loading}
				<div class="card min-h-[500px] flex flex-col items-center justify-center gap-4 text-[--color-text-secondary]">
					<div class="spinner"></div>
					<p>Executing query...</p>
				</div>
			{:else if results}
				<div class="card min-h-[500px]">
					<div class="flex justify-between items-center mb-4 pb-3 border-b border-[--color-border] flex-wrap gap-3">
						<div class="flex items-center gap-4 flex-wrap">
							<h2 class="text-base font-semibold">Results</h2>
							<div class="flex gap-2 flex-wrap">
								<span class="text-xs text-[--color-text-muted] bg-[--color-bg-tertiary] px-2 py-1 rounded-[--radius]">{results.count} rows</span>
								<span class="text-xs text-[--color-text-muted] bg-[--color-bg-tertiary] px-2 py-1 rounded-[--radius]">{results.time}</span>
								{#if graphNodes.length > 0}
									<span class="text-xs text-[--color-text-muted] bg-[--color-bg-tertiary] px-2 py-1 rounded-[--radius]">{graphNodes.length} nodes</span>
								{/if}
								{#if graphEdges.length > 0}
									<span class="text-xs text-[--color-text-muted] bg-[--color-bg-tertiary] px-2 py-1 rounded-[--radius]">{graphEdges.length} edges</span>
								{/if}
							</div>
						</div>
						<div class="flex items-center gap-3">
							<!-- Export buttons -->
							<div class="flex gap-1">
								<button
									class="btn btn-sm btn-ghost"
									onclick={exportToJSON}
									title="Export to JSON"
								>
									JSON
								</button>
								<button
									class="btn btn-sm btn-ghost"
									onclick={exportToCSV}
									title="Export to CSV"
								>
									CSV
								</button>
							</div>
							<!-- View toggle -->
							<div class="flex bg-[--color-bg] rounded-[--radius] p-0.5">
								<button
									class="px-3 py-1.5 bg-transparent border-none rounded-[calc(var(--radius)-2px)] text-sm cursor-pointer transition-all text-[--color-text-secondary] hover:text-[--color-text] disabled:opacity-50 disabled:cursor-not-allowed"
									class:bg-[--color-primary]={viewMode === 'graph'}
									class:text-white={viewMode === 'graph'}
									onclick={() => (viewMode = 'graph')}
									disabled={graphNodes.length === 0}
								>
									Graph
								</button>
								<button
									class="px-3 py-1.5 bg-transparent border-none rounded-[calc(var(--radius)-2px)] text-sm cursor-pointer transition-all text-[--color-text-secondary] hover:text-[--color-text]"
									class:bg-[--color-primary]={viewMode === 'table'}
									class:text-white={viewMode === 'table'}
									onclick={() => (viewMode = 'table')}
								>
									Table
								</button>
							</div>
						</div>
					</div>

					{#if results.rows.length === 0}
						<div class="flex items-center justify-center min-h-[400px] text-[--color-text-muted]">
							<p>No results found</p>
						</div>
					{:else if viewMode === 'graph' && graphNodes.length > 0}
						<div class="relative">
							<div class="flex gap-2 mb-3">
								<button class="btn btn-sm btn-secondary" onclick={() => graphComponent?.resetZoom()}>
									Reset Zoom
								</button>
								<button class="btn btn-sm btn-secondary" onclick={() => graphComponent?.reheat()}>
									Reheat Layout
								</button>
							</div>
							<GraphVisualization
								bind:this={graphComponent}
								nodes={graphNodes}
								edges={graphEdges}
								width={800}
								height={500}
								onNodeClick={handleNodeClick}
								onEdgeClick={handleEdgeClick}
							/>
							<p class="text-xs text-[--color-text-muted] mt-2">Click on a node or edge to edit</p>
						</div>
					{:else}
						<div class="table-container">
							<table class="table text-sm">
								<thead>
									<tr>
										{#each results.columns as col}
											<th>{col}</th>
										{/each}
									</tr>
								</thead>
								<tbody>
									{#each results.rows as row}
										<tr>
											{#each results.columns as col}
												<td>
													<div class="max-w-[400px] overflow-hidden">
														{#if typeof row[col] === 'object'}
															<pre class="text-xs m-0 p-2 bg-[--color-bg] rounded-[--radius] max-h-[150px] overflow-auto">{JSON.stringify(row[col], null, 2)}</pre>
														{:else}
															{row[col]}
														{/if}
													</div>
												</td>
											{/each}
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					{/if}
				</div>
			{:else}
				<div class="card min-h-[500px] flex items-center justify-center">
					<div class="text-center text-[--color-text-muted]">
						<div class="text-[--color-primary] mb-4">
							<svg width="64" height="64" viewBox="0 0 64 64" fill="none">
								<circle cx="32" cy="32" r="28" stroke="currentColor" stroke-width="2" opacity="0.3"/>
								<circle cx="20" cy="24" r="6" fill="currentColor" opacity="0.5"/>
								<circle cx="44" cy="24" r="6" fill="currentColor" opacity="0.5"/>
								<circle cx="32" cy="44" r="6" fill="currentColor" opacity="0.5"/>
								<line x1="24" y1="28" x2="28" y2="40" stroke="currentColor" stroke-width="2" opacity="0.3"/>
								<line x1="40" y1="28" x2="36" y2="40" stroke="currentColor" stroke-width="2" opacity="0.3"/>
								<line x1="26" y1="24" x2="38" y2="24" stroke="currentColor" stroke-width="2" opacity="0.3"/>
							</svg>
						</div>
						<h3 class="text-[--color-text-secondary] mb-2">Ready to Explore</h3>
						<p class="mb-1">Execute a query to visualize your graph data</p>
						<p class="text-xs text-[--color-text-muted]">Try "Graph with relationships" from examples</p>
					</div>
				</div>
			{/if}
		</div>
	</div>
</div>

<!-- Node Edit Modal -->
{#if showNodeModal}
	<div
		class="fixed inset-0 bg-black/70 flex items-center justify-center z-50"
		onclick={() => (showNodeModal = false)}
		onkeydown={(e) => e.key === 'Escape' && (showNodeModal = false)}
		role="dialog"
		aria-modal="true"
		tabindex="-1"
	>
		<div
			class="bg-[--color-bg-secondary] border border-[--color-border] rounded-[--radius-lg] w-full max-w-md max-h-[90vh] overflow-y-auto"
			onclick={(e) => e.stopPropagation()}
			role="document"
		>
			<div class="flex justify-between items-center p-5 border-b border-[--color-border]">
				<h2 class="text-lg font-semibold">{editingNode ? `Edit Node ${editingNode.id}` : 'Create Node'}</h2>
				<button class="btn btn-ghost btn-icon" onclick={() => (showNodeModal = false)}>
					✕
				</button>
			</div>

			<form class="p-5" onsubmit={(e) => { e.preventDefault(); saveNode(); }}>
				<div class="mb-4">
					<label class="label" for="node-labels">Labels (comma-separated)</label>
					<input
						id="node-labels"
						type="text"
						class="input"
						bind:value={nodeForm.labels}
						placeholder="Person, Employee"
					/>
				</div>

				<div class="mb-4">
					<label class="label" for="node-properties">Properties (JSON)</label>
					<textarea
						id="node-properties"
						class="input font-mono text-sm"
						bind:value={nodeForm.properties}
						rows="6"
						placeholder={'{"name": "John", "age": 30}'}
					></textarea>
				</div>

				<div class="flex justify-between gap-3">
					{#if editingNode}
						<button type="button" class="btn btn-ghost text-[--color-error]" onclick={confirmDeleteNode} disabled={savingNode}>
							Delete
						</button>
					{:else}
						<div></div>
					{/if}
					<div class="flex gap-2">
						<button type="button" class="btn btn-secondary" onclick={() => (showNodeModal = false)} disabled={savingNode}>
							Cancel
						</button>
						<button type="submit" class="btn btn-primary" disabled={savingNode}>
							{#if savingNode}
								<span class="inline-block w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin mr-2"></span>
							{/if}
							{editingNode ? 'Update' : 'Create'}
						</button>
					</div>
				</div>
			</form>
		</div>
	</div>
{/if}

<!-- Delete Node Confirmation -->
<ConfirmDialog
	bind:open={showDeleteNodeConfirm}
	title="Delete Node"
	message="Are you sure you want to delete node {editingNode?.id}? This action cannot be undone and will also remove all connected edges."
	confirmText="Delete Node"
	variant="danger"
	onConfirm={deleteNode}
/>

<!-- Edge Edit Modal -->
{#if showEdgeModal}
	<div
		class="fixed inset-0 bg-black/70 flex items-center justify-center z-50"
		onclick={() => (showEdgeModal = false)}
		onkeydown={(e) => e.key === 'Escape' && (showEdgeModal = false)}
		role="dialog"
		aria-modal="true"
		tabindex="-1"
	>
		<div
			class="bg-[--color-bg-secondary] border border-[--color-border] rounded-[--radius-lg] w-full max-w-md max-h-[90vh] overflow-y-auto"
			onclick={(e) => e.stopPropagation()}
			role="document"
		>
			<div class="flex justify-between items-center p-5 border-b border-[--color-border]">
				<h2 class="text-lg font-semibold">{editingEdge ? 'Edit Edge' : 'Create Edge'}</h2>
				<button class="btn btn-ghost btn-icon" onclick={() => (showEdgeModal = false)}>
					✕
				</button>
			</div>

			<form class="p-5" onsubmit={(e) => { e.preventDefault(); saveEdge(); }}>
				<div class="mb-4">
					<label class="label" for="edge-type">Relationship Type</label>
					<input
						id="edge-type"
						type="text"
						class="input"
						bind:value={edgeForm.type}
						placeholder="KNOWS"
					/>
				</div>

				<div class="grid grid-cols-2 gap-4 mb-4">
					<div>
						<label class="label" for="edge-from">From Node ID</label>
						<input
							id="edge-from"
							type="text"
							class="input"
							bind:value={edgeForm.fromId}
							placeholder="1"
							disabled={!!editingEdge}
						/>
					</div>
					<div>
						<label class="label" for="edge-to">To Node ID</label>
						<input
							id="edge-to"
							type="text"
							class="input"
							bind:value={edgeForm.toId}
							placeholder="2"
							disabled={!!editingEdge}
						/>
					</div>
				</div>

				<div class="mb-4">
					<label class="label" for="edge-properties">Properties (JSON)</label>
					<textarea
						id="edge-properties"
						class="input font-mono text-sm"
						bind:value={edgeForm.properties}
						rows="4"
						placeholder={'{"since": "2024-01-01"}'}
					></textarea>
				</div>

				<div class="flex justify-between gap-3">
					{#if editingEdge}
						<button type="button" class="btn btn-ghost text-[--color-error]" onclick={confirmDeleteEdge} disabled={savingEdge}>
							Delete
						</button>
					{:else}
						<div></div>
					{/if}
					<div class="flex gap-2">
						<button type="button" class="btn btn-secondary" onclick={() => (showEdgeModal = false)} disabled={savingEdge}>
							Cancel
						</button>
						<button type="submit" class="btn btn-primary" disabled={savingEdge}>
							{#if savingEdge}
								<span class="inline-block w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin mr-2"></span>
							{/if}
							{editingEdge ? 'Update' : 'Create'}
						</button>
					</div>
				</div>
			</form>
		</div>
	</div>
{/if}

<!-- Delete Edge Confirmation -->
<ConfirmDialog
	bind:open={showDeleteEdgeConfirm}
	title="Delete Edge"
	message="Are you sure you want to delete this edge? This action cannot be undone."
	confirmText="Delete Edge"
	variant="danger"
	onConfirm={deleteEdge}
/>
