-- Where a classroom's mark colours change, in percent (D-144): this and above
-- is green, down to band_middle amber, below that red.
ALTER TABLE grading.report_headers
    ADD COLUMN band_high   integer NOT NULL DEFAULT 75,
    ADD COLUMN band_middle integer NOT NULL DEFAULT 50,
    ADD CONSTRAINT report_headers_bands_check CHECK (band_middle >= 1 AND band_high <= 100 AND band_middle < band_high);
