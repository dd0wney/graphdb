<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import * as d3 from 'd3';

	interface GraphNode {
		id: string | number;
		labels: string[];
		properties: Record<string, unknown>;
		x?: number;
		y?: number;
		fx?: number | null;
		fy?: number | null;
	}

	interface GraphEdge {
		id: string | number;
		source: string | number | GraphNode;
		target: string | number | GraphNode;
		type: string;
		properties: Record<string, unknown>;
	}

	interface Props {
		nodes: GraphNode[];
		edges: GraphEdge[];
		width?: number;
		height?: number;
		onNodeClick?: (node: GraphNode) => void;
		onEdgeClick?: (edge: GraphEdge) => void;
	}

	let {
		nodes = [],
		edges = [],
		width = 800,
		height = 600,
		onNodeClick,
		onEdgeClick
	}: Props = $props();

	let container: HTMLDivElement;
	let svg: d3.Selection<SVGSVGElement, unknown, null, undefined>;
	let simulation: d3.Simulation<GraphNode, GraphEdge>;
	let selectedNode = $state<GraphNode | null>(null);
	let selectedEdge = $state<GraphEdge | null>(null);
	let tooltip = $state({ show: false, x: 0, y: 0, content: '' });

	// Color scale for node labels
	const colorScale = d3.scaleOrdinal(d3.schemeTableau10);

	function getNodeColor(node: GraphNode): string {
		const label = node.labels?.[0] || 'default';
		return colorScale(label);
	}

	function initGraph() {
		if (!container || nodes.length === 0) return;

		// Clear existing
		d3.select(container).selectAll('*').remove();

		// Create SVG
		svg = d3.select(container)
			.append('svg')
			.attr('width', '100%')
			.attr('height', '100%')
			.attr('viewBox', `0 0 ${width} ${height}`)
			.attr('preserveAspectRatio', 'xMidYMid meet');

		// Add zoom behavior
		const zoom = d3.zoom<SVGSVGElement, unknown>()
			.scaleExtent([0.1, 4])
			.on('zoom', (event) => {
				g.attr('transform', event.transform);
			});

		svg.call(zoom);

		// Create main group for zoom/pan
		const g = svg.append('g');

		// Add arrow marker for directed edges
		svg.append('defs').append('marker')
			.attr('id', 'arrowhead')
			.attr('viewBox', '-0 -5 10 10')
			.attr('refX', 20)
			.attr('refY', 0)
			.attr('orient', 'auto')
			.attr('markerWidth', 6)
			.attr('markerHeight', 6)
			.append('path')
			.attr('d', 'M 0,-5 L 10,0 L 0,5')
			.attr('fill', '#64748b');

		// Prepare data for D3
		const nodeMap = new Map<string | number, GraphNode>();
		const graphNodes = nodes.map(n => {
			const node = { ...n };
			nodeMap.set(n.id, node);
			return node;
		});

		const graphEdges = edges.map(e => ({
			...e,
			source: nodeMap.get(typeof e.source === 'object' ? e.source.id : e.source) || e.source,
			target: nodeMap.get(typeof e.target === 'object' ? e.target.id : e.target) || e.target
		})).filter(e => e.source && e.target);

		// Create force simulation
		simulation = d3.forceSimulation<GraphNode>(graphNodes)
			.force('link', d3.forceLink<GraphNode, GraphEdge>(graphEdges)
				.id(d => d.id)
				.distance(100))
			.force('charge', d3.forceManyBody().strength(-300))
			.force('center', d3.forceCenter(width / 2, height / 2))
			.force('collision', d3.forceCollide().radius(40));

		// Draw edges
		const edgeGroup = g.append('g').attr('class', 'edges');

		const link = edgeGroup.selectAll('line')
			.data(graphEdges)
			.join('line')
			.attr('stroke', '#475569')
			.attr('stroke-width', 2)
			.attr('marker-end', 'url(#arrowhead)')
			.style('cursor', 'pointer')
			.on('click', (event, d) => {
				event.stopPropagation();
				selectedEdge = d;
				selectedNode = null;
				onEdgeClick?.(d);
			})
			.on('mouseenter', (event, d) => {
				showTooltip(event, `${d.type}`);
			})
			.on('mouseleave', hideTooltip);

		// Edge labels
		const edgeLabels = edgeGroup.selectAll('text')
			.data(graphEdges)
			.join('text')
			.attr('class', 'edge-label')
			.attr('fill', '#94a3b8')
			.attr('font-size', '10px')
			.attr('text-anchor', 'middle')
			.text(d => d.type);

		// Draw nodes
		const nodeGroup = g.append('g').attr('class', 'nodes');

		const node = nodeGroup.selectAll<SVGGElement, GraphNode>('g')
			.data(graphNodes)
			.join('g')
			.style('cursor', 'pointer')
			.call(d3.drag<SVGGElement, GraphNode>()
				.on('start', dragstarted)
				.on('drag', dragged)
				.on('end', dragended) as any);

		// Node circles
		node.append('circle')
			.attr('r', 20)
			.attr('fill', d => getNodeColor(d))
			.attr('stroke', '#1e293b')
			.attr('stroke-width', 2);

		// Node labels (first label)
		node.append('text')
			.attr('dy', 4)
			.attr('text-anchor', 'middle')
			.attr('fill', 'white')
			.attr('font-size', '10px')
			.attr('font-weight', 'bold')
			.text(d => {
				const label = d.labels?.[0] || '';
				return label.substring(0, 3).toUpperCase();
			});

		// Node ID below
		node.append('text')
			.attr('dy', 35)
			.attr('text-anchor', 'middle')
			.attr('fill', '#94a3b8')
			.attr('font-size', '10px')
			.text(d => `#${d.id}`);

		// Node interactions
		node.on('click', (event, d) => {
			event.stopPropagation();
			selectedNode = d;
			selectedEdge = null;
			onNodeClick?.(d);
		})
		.on('mouseenter', (event, d) => {
			const props = Object.entries(d.properties || {})
				.slice(0, 3)
				.map(([k, v]) => `${k}: ${v}`)
				.join('\n');
			showTooltip(event, `${d.labels?.join(', ') || 'Node'}\n${props || 'No properties'}`);
		})
		.on('mouseleave', hideTooltip);

		// Background click to deselect
		svg.on('click', () => {
			selectedNode = null;
			selectedEdge = null;
		});

		// Update positions on tick
		simulation.on('tick', () => {
			link
				.attr('x1', d => (d.source as GraphNode).x || 0)
				.attr('y1', d => (d.source as GraphNode).y || 0)
				.attr('x2', d => (d.target as GraphNode).x || 0)
				.attr('y2', d => (d.target as GraphNode).y || 0);

			edgeLabels
				.attr('x', d => ((d.source as GraphNode).x! + (d.target as GraphNode).x!) / 2)
				.attr('y', d => ((d.source as GraphNode).y! + (d.target as GraphNode).y!) / 2);

			node.attr('transform', d => `translate(${d.x || 0},${d.y || 0})`);
		});

		function dragstarted(event: d3.D3DragEvent<SVGGElement, GraphNode, GraphNode>) {
			if (!event.active) simulation.alphaTarget(0.3).restart();
			event.subject.fx = event.subject.x;
			event.subject.fy = event.subject.y;
		}

		function dragged(event: d3.D3DragEvent<SVGGElement, GraphNode, GraphNode>) {
			event.subject.fx = event.x;
			event.subject.fy = event.y;
		}

		function dragended(event: d3.D3DragEvent<SVGGElement, GraphNode, GraphNode>) {
			if (!event.active) simulation.alphaTarget(0);
			event.subject.fx = null;
			event.subject.fy = null;
		}
	}

	function showTooltip(event: MouseEvent, content: string) {
		const rect = container.getBoundingClientRect();
		tooltip = {
			show: true,
			x: event.clientX - rect.left + 10,
			y: event.clientY - rect.top - 10,
			content
		};
	}

	function hideTooltip() {
		tooltip = { ...tooltip, show: false };
	}

	export function resetZoom() {
		if (svg) {
			svg.transition()
				.duration(750)
				.call(d3.zoom<SVGSVGElement, unknown>().transform as any, d3.zoomIdentity);
		}
	}

	export function reheat() {
		if (simulation) {
			simulation.alpha(1).restart();
		}
	}

	$effect(() => {
		if (nodes && edges) {
			initGraph();
		}
	});

	onDestroy(() => {
		if (simulation) {
			simulation.stop();
		}
	});
</script>

<div class="w-full h-full min-h-[400px] bg-[--color-bg] rounded-[--radius] relative overflow-hidden" bind:this={container}>
	{#if nodes.length === 0}
		<div class="absolute inset-0 flex flex-col items-center justify-center text-[--color-text-muted]">
			<p>No graph data to visualize</p>
			<p class="text-sm mt-2">Execute a query that returns nodes and relationships</p>
		</div>
	{/if}

	{#if tooltip.show}
		<div
			class="absolute bg-[--color-bg-secondary] border border-[--color-border] rounded-[--radius] p-2 px-3 text-xs pointer-events-none z-50 max-w-[250px] whitespace-pre-wrap break-words"
			style="left: {tooltip.x}px; top: {tooltip.y}px"
		>
			{#each tooltip.content.split('\n') as line}
				<div>{line}</div>
			{/each}
		</div>
	{/if}
</div>

{#if selectedNode || selectedEdge}
	<div class="absolute right-4 top-4 w-[280px] bg-[--color-bg-secondary] border border-[--color-border] rounded-[--radius-lg] p-4 max-h-[400px] overflow-y-auto">
		{#if selectedNode}
			<h3 class="text-base font-semibold mb-3">Node #{selectedNode.id}</h3>
			<div class="flex flex-wrap gap-1.5 mb-4">
				{#each selectedNode.labels || [] as label}
					<span class="px-2 py-0.5 rounded-full text-xs text-white" style="background: {getNodeColor(selectedNode)}">{label}</span>
				{/each}
			</div>
			<div class="mt-3">
				<h4 class="text-xs font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-2">Properties</h4>
				{#if Object.keys(selectedNode.properties || {}).length > 0}
					{#each Object.entries(selectedNode.properties || {}) as [key, value]}
						<div class="flex gap-2 py-1.5 border-b border-[--color-border] last:border-b-0 text-sm">
							<span class="text-[--color-text-secondary] shrink-0">{key}:</span>
							<span class="font-mono break-all">{JSON.stringify(value)}</span>
						</div>
					{/each}
				{:else}
					<p class="text-sm text-[--color-text-muted]">No properties</p>
				{/if}
			</div>
		{:else if selectedEdge}
			<h3 class="text-base font-semibold mb-3">Edge #{selectedEdge.id}</h3>
			<div class="inline-block px-3 py-1 bg-[--color-bg-tertiary] rounded-[--radius] font-mono text-sm mb-2">{selectedEdge.type}</div>
			<div class="text-sm text-[--color-text-secondary] mb-4">
				{(selectedEdge.source as GraphNode).id} → {(selectedEdge.target as GraphNode).id}
			</div>
			<div class="mt-3">
				<h4 class="text-xs font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-2">Properties</h4>
				{#if Object.keys(selectedEdge.properties || {}).length > 0}
					{#each Object.entries(selectedEdge.properties || {}) as [key, value]}
						<div class="flex gap-2 py-1.5 border-b border-[--color-border] last:border-b-0 text-sm">
							<span class="text-[--color-text-secondary] shrink-0">{key}:</span>
							<span class="font-mono break-all">{JSON.stringify(value)}</span>
						</div>
					{/each}
				{:else}
					<p class="text-sm text-[--color-text-muted]">No properties</p>
				{/if}
			</div>
		{/if}
	</div>
{/if}

<style>
	/* SVG needs to be block display for proper sizing */
	:global(.graph-container svg) {
		display: block;
	}
</style>
