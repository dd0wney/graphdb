<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { EditorView, keymap, placeholder as placeholderExt, lineNumbers, highlightActiveLineGutter, highlightSpecialChars, drawSelection, dropCursor, rectangularSelection, crosshairCursor, highlightActiveLine } from '@codemirror/view';
	import { EditorState, Compartment } from '@codemirror/state';
	import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
	import { syntaxHighlighting, HighlightStyle, bracketMatching, foldGutter, foldKeymap } from '@codemirror/language';
	import { sql, StandardSQL } from '@codemirror/lang-sql';
	import { autocompletion, completionKeymap, closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete';
	import { tags as t } from '@lezer/highlight';

	interface Props {
		value: string;
		placeholder?: string;
		onExecute?: () => void;
		onValueChange?: (value: string) => void;
	}

	let { value = $bindable(), placeholder = 'Enter your Cypher query...', onExecute, onValueChange }: Props = $props();

	let container: HTMLDivElement;
	let view: EditorView | null = null;
	let themeCompartment = new Compartment();

	// Cypher-like keywords for autocomplete
	const cypherKeywords = [
		'MATCH', 'WHERE', 'RETURN', 'WITH', 'ORDER', 'BY', 'SKIP', 'LIMIT',
		'CREATE', 'DELETE', 'SET', 'REMOVE', 'MERGE', 'ON', 'DETACH',
		'OPTIONAL', 'CALL', 'YIELD', 'UNION', 'UNWIND', 'AS', 'DISTINCT',
		'AND', 'OR', 'NOT', 'IN', 'IS', 'NULL', 'TRUE', 'FALSE',
		'CASE', 'WHEN', 'THEN', 'ELSE', 'END', 'DESC', 'ASC',
		'STARTS', 'ENDS', 'CONTAINS', 'EXISTS', 'ALL', 'ANY', 'NONE', 'SINGLE',
		'FOREACH', 'LOAD', 'CSV', 'FROM', 'HEADERS', 'INDEX', 'CONSTRAINT',
		'UNIQUE', 'ASSERT', 'DROP', 'USING', 'SCAN', 'JOIN',
		'id', 'type', 'labels', 'keys', 'properties', 'nodes', 'relationships', 'path',
		'count', 'sum', 'avg', 'min', 'max', 'collect', 'head', 'tail', 'last', 'size', 'length',
		'toInteger', 'toFloat', 'toString', 'toBoolean', 'coalesce', 'timestamp', 'date', 'datetime'
	];

	function cypherCompletions(context: any) {
		const word = context.matchBefore(/\w*/);
		if (!word || (word.from === word.to && !context.explicit)) return null;

		const filtered = cypherKeywords
			.filter(k => k.toLowerCase().startsWith(word.text.toLowerCase()))
			.map(label => ({ label, type: label === label.toUpperCase() ? 'keyword' : 'function' }));

		return {
			from: word.from,
			options: filtered
		};
	}

	// Theme for dark mode
	const darkTheme = EditorView.theme({
		'&': {
			backgroundColor: 'var(--color-bg)',
			color: 'var(--color-text)',
			fontSize: '14px',
			fontFamily: 'ui-monospace, SFMono-Regular, SF Mono, Menlo, Consolas, Liberation Mono, monospace'
		},
		'.cm-content': {
			caretColor: 'var(--color-primary)',
			padding: '12px'
		},
		'.cm-cursor, .cm-dropCursor': {
			borderLeftColor: 'var(--color-primary)'
		},
		'.cm-selectionBackground, .cm-content ::selection': {
			backgroundColor: 'var(--color-primary-alpha, rgba(99, 102, 241, 0.3))'
		},
		'.cm-activeLine': {
			backgroundColor: 'rgba(255, 255, 255, 0.05)'
		},
		'.cm-gutters': {
			backgroundColor: 'var(--color-bg)',
			color: 'var(--color-text-muted)',
			border: 'none',
			borderRight: '1px solid var(--color-border)'
		},
		'.cm-activeLineGutter': {
			backgroundColor: 'rgba(255, 255, 255, 0.05)'
		},
		'.cm-foldPlaceholder': {
			backgroundColor: 'var(--color-bg-tertiary)',
			border: 'none',
			color: 'var(--color-text-muted)'
		},
		'.cm-tooltip': {
			backgroundColor: 'var(--color-bg-secondary)',
			border: '1px solid var(--color-border)',
			borderRadius: 'var(--radius)'
		},
		'.cm-tooltip.cm-tooltip-autocomplete': {
			'& > ul': {
				fontFamily: 'ui-monospace, SFMono-Regular, SF Mono, Menlo, Consolas, Liberation Mono, monospace'
			},
			'& > ul > li': {
				padding: '4px 8px'
			},
			'& > ul > li[aria-selected]': {
				backgroundColor: 'var(--color-primary)',
				color: 'white'
			}
		},
		'.cm-completionLabel': {
			color: 'var(--color-text)'
		},
		'.cm-completionDetail': {
			color: 'var(--color-text-muted)',
			fontStyle: 'italic'
		},
		'.cm-scroller': {
			overflow: 'auto',
			minHeight: '120px',
			maxHeight: '300px'
		},
		'&.cm-focused': {
			outline: 'none'
		},
		'&.cm-focused .cm-selectionBackground, ::selection': {
			backgroundColor: 'var(--color-primary-alpha, rgba(99, 102, 241, 0.3))'
		}
	}, { dark: true });

	// Syntax highlighting for Cypher-like queries
	const highlightStyle = HighlightStyle.define([
		{ tag: t.keyword, color: '#c678dd' },
		{ tag: t.operator, color: '#56b6c2' },
		{ tag: t.string, color: '#98c379' },
		{ tag: t.number, color: '#d19a66' },
		{ tag: t.bool, color: '#d19a66' },
		{ tag: t.null, color: '#d19a66' },
		{ tag: t.function(t.variableName), color: '#61afef' },
		{ tag: t.variableName, color: '#e06c75' },
		{ tag: t.propertyName, color: '#e5c07b' },
		{ tag: t.comment, color: '#5c6370', fontStyle: 'italic' },
		{ tag: t.punctuation, color: '#abb2bf' },
		{ tag: t.bracket, color: '#abb2bf' },
		{ tag: t.labelName, color: '#61afef' }
	]);

	onMount(() => {
		const executeKeymap = keymap.of([
			{
				key: 'Mod-Enter',
				run: () => {
					onExecute?.();
					return true;
				}
			}
		]);

		const state = EditorState.create({
			doc: value,
			extensions: [
				lineNumbers(),
				highlightActiveLineGutter(),
				highlightSpecialChars(),
				history(),
				foldGutter(),
				drawSelection(),
				dropCursor(),
				EditorState.allowMultipleSelections.of(true),
				bracketMatching(),
				closeBrackets(),
				autocompletion({
					override: [cypherCompletions]
				}),
				rectangularSelection(),
				crosshairCursor(),
				highlightActiveLine(),
				keymap.of([
					...closeBracketsKeymap,
					...defaultKeymap,
					...historyKeymap,
					...foldKeymap,
					...completionKeymap
				]),
				executeKeymap,
				sql({ dialect: StandardSQL }),
				syntaxHighlighting(highlightStyle),
				themeCompartment.of(darkTheme),
				placeholderExt(placeholder),
				EditorView.updateListener.of((update) => {
					if (update.docChanged) {
						const newValue = update.state.doc.toString();
						if (newValue !== value) {
							value = newValue;
							onValueChange?.(newValue);
						}
					}
				})
			]
		});

		view = new EditorView({
			state,
			parent: container
		});
	});

	onDestroy(() => {
		view?.destroy();
	});

	// Update editor when value changes externally
	$effect(() => {
		if (view && view.state.doc.toString() !== value) {
			view.dispatch({
				changes: { from: 0, to: view.state.doc.length, insert: value }
			});
		}
	});
</script>

<div
	bind:this={container}
	class="border border-[--color-border] rounded-[--radius] overflow-hidden focus-within:border-[--color-primary] transition-colors"
></div>
