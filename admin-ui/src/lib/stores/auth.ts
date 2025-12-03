// Auth store for managing authentication state

import { writable, derived } from 'svelte/store';
import type { User } from '$lib/api/types';
import { api } from '$lib/api/client';

interface AuthState {
	user: User | null;
	loading: boolean;
	error: string | null;
}

function createAuthStore() {
	const { subscribe, set, update } = writable<AuthState>({
		user: null,
		loading: true,
		error: null
	});

	return {
		subscribe,
		async init() {
			if (api.isAuthenticated()) {
				try {
					const user = await api.getCurrentUser();
					set({ user, loading: false, error: null });
				} catch {
					set({ user: null, loading: false, error: null });
				}
			} else {
				set({ user: null, loading: false, error: null });
			}
		},
		async login(username: string, password: string) {
			update((state) => ({ ...state, loading: true, error: null }));
			try {
				const response = await api.login({ username, password });
				set({ user: response.user, loading: false, error: null });
				return true;
			} catch (err) {
				const message = err instanceof Error ? err.message : 'Login failed';
				set({ user: null, loading: false, error: message });
				return false;
			}
		},
		async logout() {
			await api.logout();
			set({ user: null, loading: false, error: null });
		},
		clearError() {
			update((state) => ({ ...state, error: null }));
		}
	};
}

export const auth = createAuthStore();
export const isAuthenticated = derived(auth, ($auth) => !!$auth.user);
export const isAdmin = derived(auth, ($auth) => $auth.user?.role === 'admin');
