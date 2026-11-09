-- A report of part of the classroom can list only the students who took part (D-163).
ALTER TABLE grading.report_options ADD COLUMN attended_only boolean NOT NULL DEFAULT false;
