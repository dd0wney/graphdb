<script lang="ts">
	import { api } from '$lib/api/client';
	import { toast } from '$lib/stores/toast';

	let downloading = $state(false);

	async function downloadBackup() {
		downloading = true;
		try {
			const blob = await api.downloadBackup();
			const ts = new Date().toISOString().replace(/[:.]/g, '-');
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = `graphdb-backup-${ts}.tar.gz`;
			document.body.appendChild(a);
			a.click();
			a.remove();
			URL.revokeObjectURL(url);
			toast.success('Backup downloaded');
		} catch (e) {
			toast.error(e instanceof Error ? e.message : 'Backup failed');
		} finally {
			downloading = false;
		}
	}
</script>

<div class="max-w-3xl">
	<header class="mb-8">
		<h1 class="text-2xl font-bold mb-1">Backup &amp; Restore</h1>
		<p class="text-[--color-text-secondary]">
			Download a snapshot-consistent archive of the running store. Restore is an offline operation.
		</p>
	</header>

	<!-- Hot backup -->
	<section class="card mb-6">
		<h2 class="text-sm font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-4">
			Hot backup
		</h2>
		<p class="text-sm text-[--color-text-secondary] mb-4">
			Captures a consistent <code>.tar.gz</code> (snapshot + WAL + auth + LSA + manifest) without
			stopping the server, and downloads it to this browser. The archive contains
			<strong>every tenant's data and credential hashes</strong> — store it like a production
			database dump, encrypted at rest.
		</p>
		<button class="btn btn-primary" onclick={downloadBackup} disabled={downloading}>
			{downloading ? 'Preparing…' : 'Download backup'}
		</button>
	</section>

	<!-- Restore (offline) -->
	<section class="card mb-6">
		<h2 class="text-sm font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-4">
			Restore (offline)
		</h2>
		<p class="text-sm text-[--color-text-secondary] mb-4">
			Restore replaces the data directory and requires a server restart, so it cannot be done from
			this dashboard. Verify the archive's integrity and restore it with the admin CLI:
		</p>
		<pre class="bg-[--color-bg-tertiary] rounded-lg p-4 text-sm overflow-x-auto"><code>{`# 1. Stop the server
# 2. Verify integrity (optional but recommended)
graphdb-admin backup verify graphdb-backup-<ts>.tar.gz

# 3. Validate mode + restore into the data directory
graphdb-admin backup restore --into /data --dry-run graphdb-backup-<ts>.tar.gz
graphdb-admin backup restore --into /data graphdb-backup-<ts>.tar.gz

# 4. Start the server (loads the snapshot + replays the WAL)`}</code></pre>
		<p class="text-sm text-[--color-text-muted] mt-4">
			See <code>docs/BACKUP_RESTORE.md</code> for the full runbook, including the manual
			<code>tar</code> fallback and the snapshot-mode compatibility rules.
		</p>
	</section>

	<!-- Scheduling / retention note -->
	<section class="card">
		<h2 class="text-sm font-semibold text-[--color-text-secondary] uppercase tracking-wider mb-4">
			Scheduled &amp; remote backups
		</h2>
		<p class="text-sm text-[--color-text-secondary]">
			Scheduling, retention, incremental backups, and remote targets (e.g. S3/R2) are not part of
			the open-source build. Automate the endpoint with a cron job, or use an enterprise backup
			plugin for managed, scheduled, remote backups.
		</p>
	</section>
</div>
