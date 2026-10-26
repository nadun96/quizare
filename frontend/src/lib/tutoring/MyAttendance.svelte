<script lang="ts">
	// A student's own attendance at tutoring sessions, per classroom, over the
	// last year (D-87, TS-FR-81). Shown when tutoring is on and they have any.
	import { TutorClient, tutoringConfig } from './client';
	import type { MyClassroomAttendance } from './types';

	const client = new TutorClient();
	let list = $state<MyClassroomAttendance[]>([]);
	$effect(() => {
		tutoringConfig().then(async (c) => {
			if (c.enabled) list = (await client.get<{ classrooms: MyClassroomAttendance[] }>('/api/my/attendance')).classrooms;
		}).catch(() => {});
	});
	const rate = (a: MyClassroomAttendance) => Math.round((a.attended / a.sessions) * 100);
</script>

{#if list.length}
	<section class="vstack gap-2" aria-labelledby="my-att-h">
		<h2 class="m-0" id="my-att-h">My attendance at tutoring</h2>
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack gap-3">
			{#each list as a (a.classroom_id)}
				<details>
					<summary class="row gap-2"><strong class="flex-1">{a.classroom_name}</strong><span class="tabular">{a.attended} of {a.sessions} sessions · {rate(a)}% · {Math.round(a.seconds / 60)} min</span></summary>
					<ul class="m-0 mt-2 ps-4 small">
						{#each a.recent as v (v.started_at)}
							<li>{new Date(v.started_at).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })} · {v.title} · {v.attended ? `present, ${Math.round(v.seconds / 60)} min` : 'absent'}</li>
						{/each}
					</ul>
				</details>
			{/each}
			<p class="small muted m-0">Over the last year. You count as present when you were in the session while it was live.</p>
		</div>
	</section>
{/if}
