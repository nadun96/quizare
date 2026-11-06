-- A class report's weights become fractions of 1 (D-142): a mark's percentage
-- times 0.4 is what the item gives. Saved weights were out of 100, or more in a
-- few classrooms, so each classroom's are divided by 100 or its largest weight,
-- which keeps every student's final percentage as it was.
ALTER TABLE grading.report_items DROP CONSTRAINT report_items_weight_check;
ALTER TABLE grading.report_items ALTER COLUMN weight TYPE numeric(8, 4);
UPDATE grading.report_items r
SET weight = round(r.weight / greatest(100, m.largest), 4)
FROM (SELECT classroom_id, max(weight) AS largest FROM grading.report_items GROUP BY classroom_id) m
WHERE m.classroom_id = r.classroom_id;
ALTER TABLE grading.report_items ADD CONSTRAINT report_items_weight_check CHECK (weight >= 0 AND weight <= 1);
