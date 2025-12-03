// Theme store for dark/light mode
import { writable } from 'svelte/store';
import { browser } from '$app/environment';

export type Theme = 'dark' | 'light' | 'system';

function getInitialTheme(): Theme {
	if (!browser) return 'dark';
	const stored = localStorage.getItem('theme') as Theme | null;
	return stored || 'dark';
}

function createThemeStore() {
	const { subscribe, set, update } = writable<Theme>(getInitialTheme());

	function setTheme(theme: Theme) {
		set(theme);
		if (browser) {
			localStorage.setItem('theme', theme);
			applyTheme(theme);
		}
	}

	function toggle() {
		update((current) => {
			const next = current === 'dark' ? 'light' : 'dark';
			if (browser) {
				localStorage.setItem('theme', next);
				applyTheme(next);
			}
			return next;
		});
	}

	function applyTheme(theme: Theme) {
		const root = document.documentElement;
		const isDark = theme === 'dark' ||
			(theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);

		root.classList.toggle('light-theme', !isDark);
		root.classList.toggle('dark-theme', isDark);
	}

	// Initialize on client
	if (browser) {
		applyTheme(getInitialTheme());

		// Listen for system theme changes
		window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
			const current = localStorage.getItem('theme') as Theme;
			if (current === 'system') {
				applyTheme('system');
			}
		});
	}

	return {
		subscribe,
		set: setTheme,
		toggle
	};
}

export const theme = createThemeStore();
