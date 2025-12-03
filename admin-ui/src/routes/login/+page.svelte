<script lang="ts">
	import { goto } from '$app/navigation';
	import { auth } from '$lib/stores/auth';

	let username = $state('');
	let password = $state('');
	let loading = $state(false);

	async function handleSubmit(e: Event) {
		e.preventDefault();
		loading = true;

		const success = await auth.login(username, password);

		loading = false;

		if (success) {
			goto('/');
		}
	}
</script>

<div class="min-h-screen flex items-center justify-center bg-[--color-bg] p-4">
	<div class="w-full max-w-md bg-[--color-bg-secondary] border border-[--color-border] rounded-xl p-8">
		<!-- Header -->
		<div class="text-center mb-8">
			<svg width="48" height="48" viewBox="0 0 32 32" fill="none" class="text-[--color-primary] mx-auto mb-4">
				<circle cx="16" cy="16" r="14" stroke="currentColor" stroke-width="2" />
				<circle cx="16" cy="10" r="3" fill="currentColor" />
				<circle cx="10" cy="20" r="3" fill="currentColor" />
				<circle cx="22" cy="20" r="3" fill="currentColor" />
				<line x1="16" y1="13" x2="10" y2="17" stroke="currentColor" stroke-width="2" />
				<line x1="16" y1="13" x2="22" y2="17" stroke="currentColor" stroke-width="2" />
				<line x1="10" y1="20" x2="22" y2="20" stroke="currentColor" stroke-width="2" />
			</svg>
			<h1 class="text-2xl font-bold mb-2">GraphDB Admin</h1>
			<p class="text-[--color-text-secondary]">Sign in to your account</p>
		</div>

		{#if $auth.error}
			<div class="alert alert-error">{$auth.error}</div>
		{/if}

		<form onsubmit={handleSubmit} class="space-y-5">
			<div>
				<label for="username" class="label">Username</label>
				<input
					type="text"
					id="username"
					class="input"
					bind:value={username}
					placeholder="Enter your username"
					required
					disabled={loading}
				/>
			</div>

			<div>
				<label for="password" class="label">Password</label>
				<input
					type="password"
					id="password"
					class="input"
					bind:value={password}
					placeholder="Enter your password"
					required
					disabled={loading}
				/>
			</div>

			<button
				type="submit"
				class="btn btn-primary w-full py-3"
				disabled={loading}
			>
				{#if loading}
					<span class="spinner w-4 h-4 border-2"></span>
					Signing in...
				{:else}
					Sign In
				{/if}
			</button>
		</form>
	</div>
</div>
