-- Erasing takes a stroke's whole gesture (D-52): find a gesture's pieces by index.
CREATE INDEX poll_board_strokes_gesture_idx ON poll.board_strokes (poll_id, gesture);
