# UX research

This page records the user-experience research behind the interface (D-39): what comparable classroom-quiz and online-exam products do, what accessibility and readability guidance requires, and the design rules this app follows as a result. It is the reference for any visual or interaction change.

## Constraints from the BA

These are requirements, not preferences:

| Source | Requirement | What it means for the UI |
|--------|-------------|--------------------------|
| NFR-13 | Student flow works on mobile browsers without an app | Design for a phone first; the teacher console must still work on a tablet |
| NFR-14 | WCAG 2.1 AA for teacher and student screens | Contrast of at least 4.5:1 for text and 3:1 for UI parts, visible focus, information never shown by colour alone |
| NFR-09 | Question load under 1.5 s on 4G | No web fonts or icon fonts, no JavaScript UI framework on the student path, and CSS kept small |
| FR-PR-01, UC-03 | Full screen, one question at a time, with its timer | The quiz screen has no site navigation; the timer is always available |
| NFR-11 | Answers saved on every change | Saving must be visible, so students trust it |

## Who uses it, and where

- **Students** use their own phones, often older Android devices on mobile data, sitting in a classroom under time pressure. They are anxious, one-handed and easily distracted.
- **Teachers** author on a laptop and run a live session from a laptop or tablet, often projecting the join screen. They glance at the dashboard while walking around the room.
- **Admins** look at the console occasionally, on a desktop.

## Findings

### 1. Exam interfaces affect anxiety and performance

A 2025 survey study of university students and instructors (Cherif et al., MuC '25) found:

- About a third of students said the exam interface hurt their performance, and a third felt anxious before and during online exams.
- About two-thirds agreed that **simpler, uncluttered visuals** would reduce stress.
- The **timer** is double-edged: some find it helpful, many find it stressful and distracting. The authors recommend an option to **show or hide** it, and most students wanted **time-left notifications**.
- Students wanted better **progress tracking**: what is answered, what is left, and an overview of the quiz.
- Being unable to go back is the biggest stressor (Novick et al., cited in the study). This app makes one-way navigation a teacher choice (BR-09), so the UI must say clearly when it applies.

### 2. Colour must never be the only signal

Kahoot! pairs every answer colour with a shape (triangle, diamond, circle, square), so colour-blind players can still match answers. It targets WCAG 2.2 AA and offers a high-contrast mode. WCAG 1.4.1 requires the same: state such as correct, wrong, submitted or invalidated is always shown by **an icon or text as well as colour**.

### 3. Touch targets

WCAG 2.2 SC 2.5.8 (AA) sets a minimum of 24×24 CSS px. SC 2.5.5 (AAA) and the platform guidelines (Apple 44 pt, Material 48 dp) recommend **44–48 px** for primary controls. Answer options and navigation buttons are the primary controls on a phone.

### 4. Readability

- Body text of at least **16 px** on mobile (Lighthouse flags smaller); questions read better at 18–20 px.
- Line height **1.5–1.7** for body text.
- Line length of **45–75 characters** (about 66 is ideal; 45–55 on phones).
- System font stacks render quickly and look native, which also helps NFR-09.

### 5. Motion

- WCAG 2.3.3 (AAA) asks that motion triggered by interaction can be turned off. The standard technique is the `prefers-reduced-motion` media query (W3C technique SCR40).
- Motion helps when it explains change: a new question arriving, an answer being saved, a student joining. It hurts when it decorates or delays. Keep transitions short (about 150–250 ms) and never block input on them.

### 6. Live classroom dashboards

Quizizz and Socrative give teachers a live grid of students with status and progress, sortable, and update it in real time so teachers can act on misconceptions during the lesson. Counts (joined, in progress, finished, flagged) are read at a glance from across the room.

## Design rules

These rules follow from the findings. New UI should follow them.

### Colour

- One calm **primary blue** for actions and focus, warm **neutrals** for surfaces, and semantic **success, warning and error** colours used only for state. Red is kept for errors, invalidations and the last moments of a timer, never for decoration.
- Light and dark themes, both checked for AA contrast (text at least 4.5:1 on its background). Students and teachers can choose light, dark or system.
- Status is always **icon + text + colour**, for example ✓ Finished, ⚠ Flagged or ⏸ Paused.

### Layout and responsiveness

- Mobile first, with breakpoints at 640, 768 and 1024 px. No horizontal scrolling at 320 px wide; wide tables scroll inside their own container.
- Reading width is capped at about 70 characters for question text and docs; dashboards use the full width.
- The quiz screen is a single column: a sticky header with progress, time and save state, the question in the middle, and a sticky action bar at the bottom within thumb reach.
- Primary controls are at least 48 px tall; secondary controls at least 32 px with enough spacing (2.5.8).

### Typography

- System font stack, 16 px base, line height 1.6, question text 1.15–1.25 rem.
- Students can enlarge text (100%, 112% or 125%) from the account menu; layouts use `rem` so everything scales.
- Numbers that change (timers, counts) use tabular figures so they don't jitter.

### Motion and interactivity

- Transitions of 150–250 ms: questions slide in, answer selection gives immediate feedback, the save indicator confirms each save, dashboard tiles animate when they reorder, and toasts report teacher actions.
- Every transition is disabled when the user prefers reduced motion (Svelte transitions are given zero duration, CSS animations are switched off).
- Native `confirm()` dialogs are replaced by accessible modal dialogs with clear consequences ("Submit your answers? You can't change them afterwards.").
- Loading states use skeletons instead of the word "Loading…", and empty states explain what to do next.

### Exam-specific

- The progress bar shows *question n of N* and how many are answered. When going back is allowed, a question navigator shows answered, current and unanswered questions (with shapes and text as well as colour).
- The timer can be collapsed to a small pill. It expands by itself in the last minute, and time-left notices are announced through an `aria-live` region at 5 minutes, 1 minute and 30 seconds.
- Saving is always visible: *Saving…*, *Saved ✓*, or *Offline: answers will sync*.
- The quiz screen stays uncluttered: no site navigation, one question, plain language.

## UI kit choice

| Option | Runtime JavaScript | CSP (no inline script) | Theming | Fit |
|--------|-------------------|------------------------|---------|-----|
| **daisyUI 5 on Tailwind CSS 4** | None (CSS only) | Yes | CSS variables, multiple themes, OKLCH | ✔ chosen |
| shadcn-svelte (Bits UI) | Per component | Yes | CSS variables | Heavier student bundle; components copied into the repo |
| Flowbite Svelte | Per component | Yes | Tailwind config | Larger runtime; fewer exam-specific needs met |
| Skeleton | Some | Yes | Tailwind themes | Bigger CSS; different component model |

**daisyUI** was chosen because it adds no JavaScript, so the student page keeps its size budget (NFR-09). Its themes are plain CSS variables, so light and dark work with the strict CSP. Tailwind only ships the classes that are actually used, and its components (buttons, inputs, cards, alerts, badges, tabs, tables, progress, modals, toasts, stats) cover everything the app needs. Interactive behaviour (dialogs, toasts, transitions) is written in Svelte with no extra runtime.

## Sources

- Cherif et al. (2025). *Stress by Design? The Influence of Online Exam Interfaces on Student Anxiety*. Mensch und Computer 2025. [doi:10.1145/3743049.3748538](https://dl.acm.org/doi/10.1145/3743049.3748538)
- Kahoot! accessibility features and conformance: [kahoot.com/accessibility](https://kahoot.com/accessibility/) and [support article](https://support.kahoot.com/hc/en-us/articles/115004537447-Does-Kahoot-meet-accessibility-standards)
- W3C, *Understanding SC 2.5.8 Target Size (Minimum)*: [w3.org](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html)
- W3C technique SCR40, *Using the CSS prefers-reduced-motion query*: [w3.org](https://www.w3.org/WAI/WCAG21/Techniques/client-side-script/SCR40)
- Line length and readability: [UXPin, optimal line length](https://www.uxpin.com/studio/blog/optimal-line-length-for-readability/); [Government of South Australia, typography](https://accessibility.sa.gov.au/your-role/visual-design/typography)
- Live dashboards: [Socrative review (Structural Learning)](https://www.structural-learning.com/post/socrative); [Quizizz in the management classroom (SAGE, 2024)](https://journals.sagepub.com/doi/10.1177/23792981221126504)
- daisyUI themes and installation: [daisyui.com/docs/themes](https://daisyui.com/docs/themes/); [SvelteKit install guide](https://daisyui.com/blog/how-to-install-sveltekit-and-daisyui/)
