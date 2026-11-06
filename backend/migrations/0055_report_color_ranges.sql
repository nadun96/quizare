-- A classroom's own mark colours (D-146): any ranges, each with its colour, as
-- [{"from": 75, "color": "#dcfce7"}, ...], highest first, the last from 0. They
-- replace D-144's two limits, which become the same three ranges.
ALTER TABLE grading.report_headers
    ADD COLUMN color_ranges jsonb NOT NULL
        DEFAULT '[{"from": 75, "color": "#dcfce7"}, {"from": 50, "color": "#fef3c7"}, {"from": 0, "color": "#fee2e2"}]'
        CHECK (jsonb_typeof(color_ranges) = 'array');

UPDATE grading.report_headers
SET color_ranges = jsonb_build_array(
    jsonb_build_object('from', band_high, 'color', '#dcfce7'),
    jsonb_build_object('from', band_middle, 'color', '#fef3c7'),
    jsonb_build_object('from', 0, 'color', '#fee2e2'));

ALTER TABLE grading.report_headers
    DROP COLUMN band_high,
    DROP COLUMN band_middle;
