# Glossary

| Term | Meaning |
|------|---------|
| **Classroom** | A teacher's class group. Holds enrolments and modules. Has its own join code. |
| **Module** | A grouping of topics inside a classroom (a unit or a term). |
| **Topic** | A subject unit inside a module. Quizzes belong to topics. |
| **Quiz** (question series) | An ordered set of questions on a topic, with its own settings. Status `draft`, `ready` or `archived`. |
| **Question** | One item of one of seven types (`SINGLE`, `MULTI`, `MATCH`, `BLANK_OPT`, `BLANK_TEXT`, `DRAG`, `ESSAY`). Identified inside its quiz by a teacher-chosen **code** such as `Q001`. |
| **Body** | The student-visible structure of a question: options, left/right items, zones, blanks, word limit. |
| **Key** | The answer key: correct options, pairs, accepted blank answers, order, model answer, rubric. Never sent to students before release. |
| **Resource** | An image referenced by public URL and attached to a question stem (`Q`), an option (`O`) or feedback (`F`). Files are never stored (BR-15). |
| **Session** | One live run of a quiz for a class. Has a 6-character **join code** and a join URL `/j/{code}` that the QR encodes. Status `open` → `live` → `ended`. |
| **Snapshot** | A frozen copy of the quiz's questions, keys and settings layers taken when a session is created, so later quiz edits never change a running session (ADR-14). |
| **Enrolment** | A student's membership in a classroom, holding the classroom-specific **student number** (the BA calls it the classroom student ID). |
| **Attempt** | One student's participation in one session. Exactly one per student per session (BR-01). |
| **Answer** | A student's saved response to one question of an attempt, stamped with the student number (BR-03). |
| **Response** | The JSON shape of an answer: `selected`, `pairs`, `blanks`, `order` or `text` depending on type. |
| **Violation** | A detected tab switch, window blur, page close, fullscreen exit or unrecovered disconnect during a running attempt. |
| **Violation policy** | `invalidate`, `warn_then_invalidate` or `log_only`. |
| **Countdown** | The pre-start timer after admission (default 60 s). The quiz starts at zero or when the student presses Start. |
| **Overrides** | The sparse settings stored at one configuration level. |
| **Effective settings** | Overrides resolved through every level, most specific last. |
| **Mark** | The evaluation of one answer: method (`key`/`llm`/`manual`), status (`marked`, `pending`, `needs_manual`, `overridden`), score and feedback. |
| **Release** | Making results visible to students: immediately, at session end, or manually. |
| **KEK / DEK** | Key-encryption key (the master key file) and data-encryption key (random per stored API key). |
| **Share link** | A revocable, optionally expiring public link to an aggregate results view. |
